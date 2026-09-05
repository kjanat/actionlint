package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

//go:generate go run ./scripts/generate-config-schema

// IgnorePatterns is a list of regular expressions. These patterns are used for filtering errors by
// matching the error messages.
type IgnorePatterns []*regexp.Regexp

// Match returns whether the given error should be ignored due to the "ignore" configuration.
func (pats IgnorePatterns) Match(err *Error) bool {
	for _, r := range pats {
		if r.MatchString(err.Message) {
			return true
		}
	}
	return false
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (pats *IgnorePatterns) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("yaml: \"ignore\" must be a sequence node at line:%d,col:%d", n.Line, n.Column)
	}
	rs := make([]*regexp.Regexp, 0, len(n.Content))
	for _, p := range n.Content {
		r, err := regexp.Compile(p.Value)
		if err != nil {
			return fmt.Errorf("invalid regular expression %q in \"ignore\" at line%d,col:%d: %w", p.Value, n.Line, n.Column, err)
		}
		rs = append(rs, r)
	}
	*pats = rs
	return nil
}

// PathConfig is a configuration for specific file path pattern. This is for values of the "paths" mapping
// in the configuration file.
type PathConfig struct {
	// Ignore suppresses diagnostics whose message matches any of these Go regular expressions.
	//
	// Applies only to workflow files matching the parent path glob. For example,
	// `["shellcheck reported issue in this script: SC2086"]` ignores that ShellCheck diagnostic.
	// Omit this key, use `null`, or use `[]` to suppress nothing. Like the `-ignore` CLI option.
	Ignore IgnorePatterns `yaml:"ignore" jsonschema:"nullable"`
}

// Policy is the "policy" mapping in the configuration file. Each key enables one check that enforces a
// convention chosen by the repository. A key which is not set inherits its value from the configuration file
// of the next lower precedence, and all the checks are disabled when no configuration file sets them.
type Policy struct {
	// RequireCommitHash requires `uses:` references to be pinned to a full commit SHA, or an image digest
	// for Docker images, when set to `true`.
	//
	// Set `false` to disable the check. Omit this key or use `null` to leave it unset; the check is
	// disabled by default. Local references and references built with expressions are skipped.
	RequireCommitHash *bool `yaml:"require-commit-hash" jsonschema:"nullable"`
	// RequireJobTimeout requires jobs to declare `timeout-minutes` when set to `true`.
	//
	// Use `{max-minutes: 60}` to also cap the timeout at 60 minutes. The maximum must be greater
	// than zero; `{}` enables the check without a cap. Jobs calling reusable workflows are skipped.
	//
	// Set `false` to disable the check. Omit this key or use `null` to leave it unset; the check is
	// disabled by default.
	RequireJobTimeout *JobTimeoutPolicy `yaml:"require-job-timeout" jsonschema:"nullable"`
	// RequiredActions lists actions that every workflow must use in its steps.
	//
	// Write entries like `uses:` values: `actions/checkout` accepts any ref, while
	// `actions/checkout@v4*` also matches the ref. Both halves support glob patterns; `*` does not
	// match `/`. Names are matched case-insensitively and refs case-sensitively.
	//
	// Use `[]` to disable the check. Omit this key or use `null` to leave it unset; no actions
	// are required by default. Actions inside composite actions or called workflows are not counted.
	RequiredActions []string `yaml:"required-actions" jsonschema:"nullable,minLength=1"`
}

// decodeRequiredActions decodes the value of the "required-actions" key and validates every entry of
// it. A null value leaves the destination untouched so that a key set to nothing keeps the meaning of
// an absent key.
func decodeRequiredActions(n *yaml.Node, out *[]string) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("yaml: \"required-actions\" must be a sequence node at line:%d,col:%d", n.Line, n.Column)
	}
	as := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode || c.Tag != "!!str" {
			return fmt.Errorf("yaml: an entry of \"required-actions\" must be a string at line:%d,col:%d", c.Line, c.Column)
		}
		if c.Value == "" {
			return fmt.Errorf("yaml: an entry of \"required-actions\" must not be empty at line:%d,col:%d", c.Line, c.Column)
		}
		if p, ok := invalidActionPattern(c.Value); ok {
			return fmt.Errorf("yaml: invalid glob pattern %q in an entry of \"required-actions\" at line:%d,col:%d", p, c.Line, c.Column)
		}
		as = append(as, c.Value)
	}
	*out = as
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *Policy) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("yaml: \"policy\" must be a mapping node at line:%d,col:%d", n.Line, n.Column)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		var err error
		switch k.Value {
		case "require-commit-hash":
			err = v.Decode(&p.RequireCommitHash)
		case "require-job-timeout":
			err = v.Decode(&p.RequireJobTimeout)
		case "required-actions":
			err = decodeRequiredActions(v, &p.RequiredActions)
		default:
			return fmt.Errorf("yaml: unknown key %q in \"policy\" at line:%d,col:%d", k.Value, k.Line, k.Column)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// JobTimeoutPolicy is the value of the "require-job-timeout" policy in the configuration file. The
// value is a boolean which turns the check on and off, or a mapping which turns it on and sets the
// largest allowed number of minutes in its "max-minutes" key.
type JobTimeoutPolicy struct {
	enabled    bool
	maxMinutes float64
}

// RequireJobTimeout creates a JobTimeoutPolicy which turns the check on. The argument is the largest
// allowed number of minutes, where a value which is not larger than zero sets no upper limit.
func RequireJobTimeout(maxMinutes float64) *JobTimeoutPolicy {
	return &JobTimeoutPolicy{enabled: true, maxMinutes: maxMinutes}
}

// Enabled returns whether the check is turned on. It returns false when the receiver is nil.
func (p *JobTimeoutPolicy) Enabled() bool {
	return p != nil && p.enabled
}

// MaxMinutes returns the largest allowed "timeout-minutes:" value in minutes. The second return
// value is false when the policy sets no upper limit.
func (p *JobTimeoutPolicy) MaxMinutes() (float64, bool) {
	if !p.Enabled() || p.maxMinutes <= 0 {
		return 0, false
	}
	return p.maxMinutes, true
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *JobTimeoutPolicy) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&p.enabled); err != nil {
			return fmt.Errorf("yaml: \"require-job-timeout\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
		}
	case yaml.MappingNode:
		p.enabled = true
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value != "max-minutes" {
				return fmt.Errorf("yaml: unknown key %q in \"require-job-timeout\" at line:%d,col:%d", k.Value, k.Line, k.Column)
			}
			// Decoding through the YAML library reads a leading zero as YAML 1.1 octal, so
			// "max-minutes: 017" would become 15. The workflow parser reads numbers from the raw
			// text with strconv, and so does this.
			f, err := strconv.ParseFloat(v.Value, 64)
			if err != nil || v.Kind != yaml.ScalarNode {
				return fmt.Errorf("yaml: \"max-minutes\" in \"require-job-timeout\" must be a number but got %q at line:%d,col:%d", v.Value, v.Line, v.Column)
			}
			p.maxMinutes = f
			if p.maxMinutes <= 0 {
				return fmt.Errorf("yaml: \"max-minutes\" in \"require-job-timeout\" must be greater than zero but got %v at line:%d,col:%d", p.maxMinutes, v.Line, v.Column)
			}
		}
	default:
		return fmt.Errorf("yaml: \"require-job-timeout\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
	}
	return nil
}

// SelfHostedRunnerConfig is configuration for self-hosted runners.
type SelfHostedRunnerConfig struct {
	// Labels lists additional self-hosted runner labels accepted in `runs-on`.
	//
	// For example, `[linux.2xlarge, custom-*]` accepts that label and matching custom labels.
	// Patterns use Go `path.Match` syntax. Omit this key, use `null`, or use `[]` to add no labels.
	Labels []string `yaml:"labels" jsonschema:"nullable"`
}

// Config configures validation of GitHub Actions workflows for this repository.
//
// Declare custom runner labels and available variable or secret names, suppress selected
// diagnostics by file path, choose assumed token permissions, and enable repository policy checks.
// Save as `.github/actionlint.yaml` or `.github/actionlint.yml`, or select a file with `-config-file`.
// Every setting is optional; normal workflow correctness checks run without a configuration file.
type Config struct {
	// SelfHostedRunner configures extra labels accepted for self-hosted runners.
	//
	// Add your labels under `labels`, for example `{labels: [linux.2xlarge]}`.
	SelfHostedRunner SelfHostedRunnerConfig `yaml:"self-hosted-runner" jsonschema:"nullable"`
	// ConfigVariables lists configuration variable names available to the checked workflows through `vars`.
	//
	// Omit this key or use `null` to disable variable-name checking. Use `[]` to allow no variables.
	// A list such as `[DEFAULT_RUNNER, ENVIRONMENT_STAGE]` reports names outside that list as undefined.
	ConfigVariables []string `yaml:"config-variables" jsonschema:"nullable"`
	// ConfigSecrets lists secret names available to the checked workflows through `secrets`.
	//
	// Omit this key or use `null` to disable secret-name checking. Use `[]` to allow only built-in
	// secrets and secrets declared in `on.workflow_call.secrets`. A list such as `[DEPLOY_TOKEN, API_KEY]`
	// also allows those names; other names are reported as undefined. Matching is case-insensitive.
	//
	// `GITHUB_TOKEN`, `ACTIONS_STEP_DEBUG`, and `ACTIONS_RUNNER_DEBUG` are always allowed.
	// List names only, never secret values.
	ConfigSecrets []string `yaml:"config-secrets" jsonschema:"nullable"`
	// Paths applies configuration to workflow files matching a glob pattern.
	//
	// Keys are paths relative to the repository root, using `/` separators and doublestar glob syntax,
	// for example `.github/workflows/**/*.yaml`. All matching entries apply. Each entry can set `ignore`.
	Paths map[string]PathConfig `yaml:"paths" jsonschema:"nullable"`
	// AssumeDefaultPermissions selects the repository's assumed default token permissions when checking
	// reusable workflow calls whose calling job and workflow both omit `permissions:`.
	//
	// `restricted` (the default) grants read access to `contents` and `packages` only.
	// `permissive` assumes read/write access, except `id-token`, which still requires an explicit grant.
	// Omit this key or use `null` to assume `restricted`.
	AssumeDefaultPermissions DefaultPermissionsAssumption `yaml:"assume-default-permissions" jsonschema:"nullable"`
	// Policy enables opt-in checks for repository conventions, such as pinned actions and job timeouts.
	//
	// All policy checks are disabled by default. Set individual keys to enable them; omit the mapping
	// or use `{}` or `null` to leave them unset. Normal workflow correctness checks always run.
	Policy Policy `yaml:"policy" jsonschema:"nullable"`
}

// DefaultPermissionsAssumption is an assumption about the repository's "Workflow permissions" setting,
// which actionlint cannot read from a workflow file.
type DefaultPermissionsAssumption uint8

const (
	// DefaultPermissionsAssumptionUnset means the "assume-default-permissions" key was not set.
	DefaultPermissionsAssumptionUnset DefaultPermissionsAssumption = iota
	// DefaultPermissionsAssumptionRestricted assumes GitHub's restricted default token, which grants read
	// access to "contents" and "packages" and nothing else.
	DefaultPermissionsAssumptionRestricted
	// DefaultPermissionsAssumptionPermissive assumes GitHub's permissive default token.
	DefaultPermissionsAssumptionPermissive
)

// UnmarshalYAML implements yaml.Unmarshaler.
func (a *DefaultPermissionsAssumption) UnmarshalYAML(n *yaml.Node) error {
	switch n.Value {
	case "restricted":
		*a = DefaultPermissionsAssumptionRestricted
	case "permissive":
		*a = DefaultPermissionsAssumptionPermissive
	default:
		return fmt.Errorf(
			"yaml: \"assume-default-permissions\" must be \"restricted\" or \"permissive\" but got %q at line:%d,col:%d",
			n.Value,
			n.Line,
			n.Column,
		)
	}
	return nil
}

// PathConfigs returns a list of all PathConfig values matching to the given file path. The path must
// be relative to the root of the project.
func (cfg *Config) PathConfigs(path string) []PathConfig {
	path = filepath.ToSlash(path)

	var ret []PathConfig
	if cfg != nil {
		for p, c := range cfg.Paths {
			// Glob patterns were validated in `ParseConfig()`
			if doublestar.MatchUnvalidated(p, path) {
				ret = append(ret, c)
			}
		}
	}
	return ret
}

// RequiresCommitHash returns whether the "require-commit-hash" policy is enabled. It returns false
// when the receiver is nil or when the key is not set.
func (cfg *Config) RequiresCommitHash() bool {
	return cfg != nil && cfg.Policy.RequireCommitHash != nil && *cfg.Policy.RequireCommitHash
}

// RequiresJobTimeout returns the "require-job-timeout" policy. It returns nil when the receiver is
// nil or when the key is not set.
func (cfg *Config) RequiresJobTimeout() *JobTimeoutPolicy {
	if cfg == nil {
		return nil
	}
	return cfg.Policy.RequireJobTimeout
}

// RequiredActions returns the actions which every workflow must use following the "required-actions"
// policy. It returns nil when the receiver is nil or when the key is not set.
func (cfg *Config) RequiredActions() []string {
	if cfg == nil {
		return nil
	}
	return cfg.Policy.RequiredActions
}

// ParseConfig parses the given bytes as an actionlint config file. When deserializing the YAML file
// or the config validation fails, this function returns an error.
func ParseConfig(b []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		return nil, errors.New(msg)
	}
	for pat := range c.Paths {
		if !doublestar.ValidatePattern(pat) {
			return nil, fmt.Errorf("invalid glob pattern %q in \"paths\"", pat)
		}
	}
	return &c, nil
}

// ReadConfigFile reads actionlint config file (actionlint.yaml) from the given file path.
func ReadConfigFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	c, err := ParseConfig(b)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	return c, nil
}

// loadRepoConfig reads config file from the repository's .github/actionlint.yml or
// .github/actionlint.yaml.
func loadRepoConfig(root string) (*Config, error) {
	for _, f := range []string{"actionlint.yaml", "actionlint.yml"} {
		p := filepath.Join(root, ".github", f)
		c, err := ReadConfigFile(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("could not parse config file %q: %w", p, err)
		default:
			return c, nil
		}
	}
	return nil, nil
}

func writeDefaultConfigFile(path string) error {
	b := []byte(`self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels: []

# Configuration variables in array of strings defined in your repository or
# organization. ` + "`null`" + ` means disabling configuration variables check.
# Empty array means no configuration variable is allowed.
config-variables: null

# Secrets in array of strings defined in your repository or organization.
# ` + "`null`" + ` means disabling the secrets check. Empty array means no secret is
# allowed.
config-secrets: null

# Configuration for file paths. The keys are glob patterns to match to file
# paths relative to the repository root. The values are the configurations for
# the file paths. Note that the path separator is always '/'.
# The following configurations are available.
#
# "ignore" is an array of regular expression patterns. Matched error messages
# are ignored. This is similar to the "-ignore" command line option.
paths:
#  .github/workflows/**/*.yml:
#    ignore: []

# Permissions assumed for a caller workflow that declares no "permissions:"
# block at all. "restricted" (the default) assumes GitHub's restricted default
# token.
#assume-default-permissions: restricted

# Policy checks. Each key turns on one check that enforces a convention of this
# repository rather than reporting a mistake. They are all disabled when this
# mapping is absent. The keys are in alphabetical order.
#policy:
#  # Require every "uses:" to be pinned to a full commit SHA or an image
#  # digest.
#  require-commit-hash: true
#  # Require "timeout-minutes" on every job. A mapping with "max-minutes" also
#  # caps the value.
#  require-job-timeout: true
#  # Actions every workflow must use. "owner/repo@ref" also pins the version.
#  required-actions:
#    - actions/checkout
`)
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}
