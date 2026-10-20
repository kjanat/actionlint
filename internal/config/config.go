// Package config owns the repository configuration document: its model, decoding,
// validation, inheritance, overlays and inspection.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	ruffconfig "actionlint.kjanat.dev/internal/config/tools/ruff"
	configtree "actionlint.kjanat.dev/internal/config/tree"
	"actionlint.kjanat.dev/internal/fsread"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

// IgnorePatterns is a list of regular expressions. These patterns are used for filtering errors by
// matching the error messages.
type IgnorePatterns []*regexp.Regexp

// Match returns whether the given error should be ignored due to the "ignore" configuration.
func (pats IgnorePatterns) Match(message string) bool {
	for _, r := range pats {
		if r.MatchString(message) {
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

// Policy configures checks for cache safety and repository conventions.
// Cache policies are enabled by default; the remaining checks are opt-in.
// An omitted or null setting retains the check's default.
type Policy struct {
	// CacheCallUnrestricted requires explicit cache ceilings on low-trust reusable calls.
	// Enabled by default. Set false to disable it; null or omission keeps the default.
	CacheCallUnrestricted *bool `yaml:"cache-call-unrestricted" jsonschema:"nullable,default=true"`
	// CacheOperation reports official cache action steps disabled by an explicit cache mode.
	// Enabled by default. Set false to disable it; null or omission keeps the default.
	CacheOperation *bool `yaml:"cache-operation" jsonschema:"nullable,default=true"`
	// CacheWriteUntrusted reports write-capable cache modes on low-trust triggers.
	// Enabled by default. Set false to disable it; null or omission keeps the default.
	CacheWriteUntrusted *bool `yaml:"cache-write-untrusted" jsonschema:"nullable,default=true"`
	// DisallowSuppressions restricts inline and configured diagnostic exceptions.
	// `true` or `{}` reports each prohibited directive or matching config exception
	// and retains its original violations. Omission, null, or false permits exceptions.
	// Both ignore and ignore-next-line are covered equally.
	//
	// Use `{rules: [cache-call-unrestricted], report: all}` to restrict selected rule IDs.
	// Omitted rules selects all suppressible rules; an explicit list must be nonempty.
	// Report accepts suppression (exception only), violation (original findings only), or all (both kinds of diagnostic).
	DisallowSuppressions *SuppressionsPolicy `yaml:"disallow-suppressions" jsonschema:"nullable"`
	// RequireCommitHash requires `uses:` references to be pinned to a full commit SHA, or an image digest
	// for Docker images, when set to `true`.
	//
	// Set `false` to disable the check. Omit this key or use `null` to leave it unset; the check is
	// disabled by default. Local references and references built with expressions are skipped.
	RequireCommitHash *bool `yaml:"require-commit-hash" jsonschema:"nullable"`
	// RequireJobTimeout requires jobs to declare `timeout-minutes` when set to `true`.
	//
	// Use `{min-minutes: 5, max-minutes: 60}` to require a timeout between 5 and 60 minutes.
	// Either bound may be omitted. Bounds must be finite and greater than zero, and the minimum
	// must not exceed the maximum. `{}` requires the key without bounds. Reusable workflow calls are skipped.
	//
	// Set `false` to disable the check. Omit this key or use `null` to leave it unset; the check is
	// disabled by default.
	RequireJobTimeout *JobTimeoutPolicy `yaml:"require-job-timeout" jsonschema:"nullable"`
	// RequirePermissions requires an explicit `permissions:` declaration.
	//
	// `true` or `{scope: workflow}` requires a workflow-level declaration, including `permissions: {}`.
	// `{scope: job}` requires a declaration on every job, including reusable workflow calls.
	// `{}` enables workflow scope. This checks presence only; it does not infer the scopes a job needs.
	//
	// Set `false` to disable the check. Omit this key or use `null` to leave it unset; it is disabled by default.
	RequirePermissions *PermissionsPolicy `yaml:"require-permissions" jsonschema:"nullable"`
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
		if p, ok := InvalidActionPattern(c.Value); ok {
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
		case "cache-write-untrusted":
			err = v.Decode(&p.CacheWriteUntrusted)
		case "cache-call-unrestricted":
			err = v.Decode(&p.CacheCallUnrestricted)
		case "cache-operation":
			err = v.Decode(&p.CacheOperation)
		case "disallow-suppressions":
			err = v.Decode(&p.DisallowSuppressions)
		case "require-commit-hash":
			err = v.Decode(&p.RequireCommitHash)
		case "require-job-timeout":
			err = v.Decode(&p.RequireJobTimeout)
		case "require-permissions":
			err = v.Decode(&p.RequirePermissions)
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
// allowed range in its "min-minutes" and "max-minutes" keys.
type JobTimeoutPolicy struct {
	enabled    bool
	minMinutes float64
	maxMinutes float64
}

// RequireJobTimeout creates a JobTimeoutPolicy which turns the check on. The argument is the largest
// allowed number of minutes, where a value which is not larger than zero sets no upper limit.
func RequireJobTimeout(maxMinutes float64) *JobTimeoutPolicy {
	return &JobTimeoutPolicy{enabled: true, maxMinutes: maxMinutes}
}

// RequireJobTimeoutRange requires job timeouts within the inclusive bounds. Zero omits a bound.
// It rejects negative or non-finite bounds and a minimum larger than a nonzero maximum.
func RequireJobTimeoutRange(minMinutes, maxMinutes float64) (*JobTimeoutPolicy, error) {
	for _, bound := range []float64{minMinutes, maxMinutes} {
		if bound < 0 || math.IsNaN(bound) || math.IsInf(bound, 0) {
			return nil, fmt.Errorf("job timeout bounds must be finite and nonnegative, got %v", bound)
		}
	}
	if maxMinutes > 0 && minMinutes > maxMinutes {
		return nil, fmt.Errorf("minimum job timeout %v exceeds maximum %v", minMinutes, maxMinutes)
	}
	return &JobTimeoutPolicy{enabled: true, minMinutes: minMinutes, maxMinutes: maxMinutes}, nil
}

// Enabled returns whether the check is turned on. It returns false when the receiver is nil.
func (p *JobTimeoutPolicy) Enabled() bool {
	return p != nil && p.enabled
}

// MinMinutes returns the smallest allowed timeout in minutes. The boolean is false without an enabled lower bound.
func (p *JobTimeoutPolicy) MinMinutes() (float64, bool) {
	if !p.Enabled() || p.minMinutes <= 0 {
		return 0, false
	}
	return p.minMinutes, true
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
	*p = JobTimeoutPolicy{}
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&p.enabled); err != nil {
			return fmt.Errorf("yaml: \"require-job-timeout\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
		}
	case yaml.MappingNode:
		p.enabled = true
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value != "min-minutes" && k.Value != "max-minutes" {
				return fmt.Errorf("yaml: unknown key %q in \"require-job-timeout\" at line:%d,col:%d", k.Value, k.Line, k.Column)
			}
			// Decoding through the YAML library reads a leading zero as YAML 1.1 octal, so
			// "max-minutes: 017" would become 15. The workflow parser reads numbers from the raw
			// text with strconv, and so does this.
			f, err := strconv.ParseFloat(v.Value, 64)
			if err != nil || v.Kind != yaml.ScalarNode {
				return fmt.Errorf("yaml: %q in \"require-job-timeout\" must be a number but got %q at line:%d,col:%d", k.Value, v.Value, v.Line, v.Column)
			}
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return fmt.Errorf("yaml: %q in \"require-job-timeout\" must be finite but got %q at line:%d,col:%d", k.Value, v.Value, v.Line, v.Column)
			}
			if f <= 0 {
				return fmt.Errorf("yaml: %q in \"require-job-timeout\" must be greater than zero but got %v at line:%d,col:%d", k.Value, f, v.Line, v.Column)
			}
			if k.Value == "min-minutes" {
				p.minMinutes = f
			} else {
				p.maxMinutes = f
			}
		}
		if p.maxMinutes > 0 && p.minMinutes > p.maxMinutes {
			return fmt.Errorf("yaml: \"min-minutes\" must not exceed \"max-minutes\" in \"require-job-timeout\" at line:%d,col:%d", n.Line, n.Column)
		}
	default:
		return fmt.Errorf("yaml: \"require-job-timeout\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
	}
	return nil
}

// PermissionsPolicy selects where the "require-permissions" policy requires a declaration.
type PermissionsPolicy struct {
	enabled bool
	scope   string
}

// RequirePermissions enables the policy with "workflow" or "job" scope. Other values return an error.
func RequirePermissions(scope string) (*PermissionsPolicy, error) {
	if scope != "workflow" && scope != "job" {
		return nil, fmt.Errorf("permissions policy scope must be \"workflow\" or \"job\", got %q", scope)
	}
	return &PermissionsPolicy{enabled: true, scope: scope}, nil
}

// Enabled reports whether the policy is enabled. A nil receiver disables it.
func (p *PermissionsPolicy) Enabled() bool {
	return p != nil && p.enabled
}

// Scope returns "workflow" or "job", or an empty string when the policy is disabled.
func (p *PermissionsPolicy) Scope() string {
	if !p.Enabled() {
		return ""
	}
	return p.scope
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *PermissionsPolicy) UnmarshalYAML(n *yaml.Node) error {
	*p = PermissionsPolicy{scope: "workflow"}
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&p.enabled); err != nil {
			return fmt.Errorf("yaml: \"require-permissions\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
		}
	case yaml.MappingNode:
		p.enabled = true
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value != "scope" {
				return fmt.Errorf("yaml: unknown key %q in \"require-permissions\" at line:%d,col:%d", k.Value, k.Line, k.Column)
			}
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || (v.Value != "workflow" && v.Value != "job") {
				return fmt.Errorf("yaml: \"scope\" in \"require-permissions\" must be \"workflow\" or \"job\" at line:%d,col:%d", v.Line, v.Column)
			}
			p.scope = v.Value
		}
	default:
		return fmt.Errorf("yaml: \"require-permissions\" must be a boolean or a mapping at line:%d,col:%d", n.Line, n.Column)
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
// Save as `.github/actionlint.yaml`, `.github/actionlint.yml`, `.github/actionlint.json` or
// `.github/actionlint.toml`, or select a file with `--config`.
// Every setting is optional; normal workflow correctness checks run without a configuration file.
type Config struct {
	filename    string
	configFiles []string
	// Extends loads local YAML/JSON files, relative to this config, in order.
	// Later bases override earlier bases; this file wins over all bases. No URLs.
	Extends []string `yaml:"extends,omitempty" jsonschema:"nullable"`
	// Plugins declares plugin repositories and requested versions. Entries are
	// owner/repo or forge-qualified names, optionally followed by @release-tag.
	// Omitted versions select latest stable. Use a mapping with name, version,
	// and registry for a custom catalogue. Empty or null disables plugins.
	Plugins PluginDeclarations `yaml:"plugins,omitempty" jsonschema:"nullable"`
	// Files selects repository-relative inputs shared by analysis features.
	Files FilesConfig `yaml:"files,omitempty" jsonschema:"nullable"`
	// Lint configures rule selection separately from repository policy.
	Lint LintConfig `yaml:"lint" jsonschema:"nullable"`
	// Overrides applies per-file lint settings in declaration order. Later matching
	// settings win; mappings merge and arrays replace. Paths are repository-relative.
	Overrides []Override `yaml:"overrides" jsonschema:"nullable"`
	// Ignores suppresses a rule for matching files, actions, jobs, or steps.
	// Every specified selector must match. Later config files replace the list.
	Ignores IgnoreConfigs `yaml:"ignores" jsonschema:"nullable"`
	// Tools configures external linters for both the CLI and GitHub Action.
	Tools ToolsConfig `yaml:"tools" jsonschema:"nullable"`
	// LocalActions maps generated action directories to checked-in source directories.
	// Both paths are relative to the repository root, using ./ or $/ prefixes.
	// Known checkout placements are removed before matching. Sources resolve once
	// to actual directories; mappings do not chain.
	LocalActions LocalActionSources `yaml:"local-actions" jsonschema:"nullable"`
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
	// Policy configures cache safety checks and repository conventions, such as pinned actions and job timeouts.
	//
	// Cache policies default to true; other policies are opt-in. Set individual keys to override their
	// defaults. Omit the mapping or use `{}` or `null` to keep defaults. Syntax checks always run.
	Policy Policy `yaml:"policy" jsonschema:"nullable"`
}

// ToolsConfig contains configuration for external analysis tools.
type ToolsConfig struct {
	// Ruff configures checking of embedded Python scripts.
	Ruff RuffToolConfig `yaml:"ruff"`
	// Shellcheck configures checking of embedded shell scripts.
	Shellcheck ShellcheckToolConfig `yaml:"shellcheck"`
}

// RuffToolConfig selects isolated, read-only Ruff checks for Python run steps.
// Repository Ruff configuration and noqa comments are not loaded; use actionlint
// rule settings and inline suppressions for workflow exceptions.
type RuffToolConfig = ruffconfig.Config

// ShellcheckToolConfig contains ShellCheck's native configuration directives.
type ShellcheckToolConfig struct {
	// Backend selects the execution transport and its settings.
	// Explicit CLI or application invocation settings take precedence.
	Backend *toolconfig.Backend `yaml:"backend,omitempty"`
	// Enabled turns ShellCheck analysis on or off. Omission or null keeps it enabled.
	// An empty CLI command or shellcheck: false Action input still disables the tool.
	Enabled *bool `yaml:"enabled" jsonschema:"nullable,default=true"`
	// Config selects inline ShellCheck settings or an rc file/directory for each checked script.
	// File-relative paths use the directory containing actionlint.yaml; paths supplied by an overlay use the analysis working directory.
	// ${{ configdir }} explicitly selects the configuration directory; ${{ gitdir }} uses the repository root.
	// ${{ github.workspace }} selects the runner workspace or local repository; ${{ github.action_path }} requires a composite action context.
	// Lists replace during actionlint config overlays. The Action's explicit shellcheck-args can override settings.
	// Omission or null adds no directives. This does not enable rc-file discovery.
	Config *ShellcheckConfigSource `yaml:"config"`
}

// ShellcheckConfigSource selects inline directives or a configuration file/directory.
// Relative paths use their config file or overlay working directory. ${{ configdir }} and
// ${{ gitdir }} explicitly select that directory or the checked repository root.
type ShellcheckConfigSource struct {
	value     shellcheckConfigValue
	fromInput bool
}

type (
	shellcheckConfigValue interface{ shellcheckConfigValue() }
	shellcheckConfigPath  string
)

func (shellcheckConfigPath) shellcheckConfigValue() {}

type shellcheckInlineConfig struct{ ShellcheckConfig }

func (shellcheckInlineConfig) shellcheckConfigValue() {}

// ShellcheckConfig is a typed representation of project-wide ShellCheck directives.
type ShellcheckConfig struct {
	// Disable lists codes, code ranges or all, for example [SC2086, SC3000-SC4000].
	Disable []string `yaml:"disable" jsonschema:"nullable,pattern=^(all|(SC)?[0-9]+(-(SC)?[0-9]+)?)$"`
	// Enable selects optional check names. Scalar all or true expands to [all].
	// This selection does not change the tool enabled setting.
	Enable []string `yaml:"enable" jsonschema:"nullable,pattern=^[a-zA-Z-]+$"`
	// Shell overrides the inferred dialect of scripts selected for ShellCheck.
	Shell *string `yaml:"shell" jsonschema:"nullable,enum=sh,enum=bash,enum=dash,enum=ksh,enum=busybox"`
	// ExtendedAnalysis enables or disables ShellCheck dataflow analysis. Omission keeps its default.
	ExtendedAnalysis *bool `yaml:"extended-analysis" jsonschema:"nullable"`
	// ExternalSources controls following sourced files. Omission preserves actionlint's enabled default.
	ExternalSources *bool `yaml:"external-sources" jsonschema:"nullable"`
	// SourcePath lists directories searched for sourced files, relative to the run step's effective working directory.
	// Scripts arrive on stdin, so SCRIPTDIR does not denote the workflow file's directory.
	SourcePath []string `yaml:"source-path" jsonschema:"nullable,minLength=1"`
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
			// Glob patterns were validated in `Parse()`
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

// RequiresPermissions returns the "require-permissions" policy, or nil for an unset key or nil receiver.
func (cfg *Config) RequiresPermissions() *PermissionsPolicy {
	if cfg == nil {
		return nil
	}
	return cfg.Policy.RequirePermissions
}

// RequiredActions returns the actions which every workflow must use following the "required-actions"
// policy. It returns nil when the receiver is nil or when the key is not set.
func (cfg *Config) RequiredActions() []string {
	if cfg == nil {
		return nil
	}
	return cfg.Policy.RequiredActions
}

// Parse parses the given bytes as an actionlint config file. When deserializing the YAML file
// or the config validation fails, this function returns an error.
func Parse(b []byte, additional ...string) (*Config, error) {
	resolved, err := ResolveDocument(b, additional...)
	if err != nil {
		return nil, err
	}
	if len(resolved.Config.Extends) > 0 {
		return nil, errors.New("extends requires a file origin; use config.Load")
	}
	return resolved.Config, nil
}

// Resolved keeps validated values and their provenance from the same document.
type Resolved struct {
	Node     *yaml.Node
	Inputs   map[*yaml.Node]Input
	Config   *Config
	Values   map[string]any
	Origins  map[string]Origin
	Warnings []Warning
}

func ResolveDocument(b []byte, additional ...string) (Resolved, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(b, &document); err != nil {
		return Resolved{}, errors.New(strings.ReplaceAll(err.Error(), "\n", " "))
	}
	var root *yaml.Node
	if len(document.Content) > 0 {
		root = document.Content[0]
	}
	return ResolveNode(root, nil, additional...)
}

func ResolveNode(root *yaml.Node, inputs map[*yaml.Node]Input, additional ...string) (Resolved, error) {
	if inputs == nil {
		expanded, err := configtree.Expand(root, make(map[*yaml.Node]bool))
		if err != nil {
			return Resolved{}, err
		}
		root = normalizeToolSwitch(expanded)
	}
	var c Config
	if root != nil {
		if err := root.Decode(&c); err != nil {
			return Resolved{}, errors.New(strings.ReplaceAll(err.Error(), "\n", " "))
		}
		// YAML null bypasses field decoders. Preserve its explicit selection of
		// no plugins, including when it replaces an inherited declaration.
		for i := 0; root.Kind == yaml.MappingNode && i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "plugins" && root.Content[i+1].ShortTag() == "!!null" {
				c.Plugins = PluginDeclarations{}
			}
		}
	}
	if err := ValidateRuleNames(&c, additional...); err != nil {
		return Resolved{}, err
	}
	for pat := range c.Paths {
		if !doublestar.ValidatePattern(pat) {
			return Resolved{}, fmt.Errorf("invalid glob pattern %q in \"paths\"", pat)
		}
	}
	values, err := EffectiveValues(&c)
	if err != nil {
		return Resolved{}, err
	}
	origins := map[string]Origin{}
	configDefaultOrigins(values, "", origins)
	if err := configOrigins(root, "", origins, inputs); err != nil {
		return Resolved{}, err
	}
	if c.Tools.Shellcheck.Config != nil {
		c.Tools.Shellcheck.Config.fromInput = origins["/tools/shellcheck/config"].Source == "input"
	}
	for i := range c.Overrides {
		tools := c.Overrides[i].Tools
		if tools != nil && tools.Shellcheck.Config != nil {
			tools.Shellcheck.Config.fromInput = origins["/overrides"].Source == "input"
		}
	}
	return Resolved{Node: root, Inputs: inputs, Config: &c, Values: values, Origins: origins, Warnings: configWarnings(root, inputs)}, nil
}

// Load reads the actionlint config file (actionlint.yaml) from the given file path.
func Load(path string, additional ...string) (*Config, error) {
	source, err := Read(path, fsread.Disk, additional...)
	if err != nil {
		return nil, err
	}
	return source.Config, nil
}

func Read(path string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, error) {
	warningsByFile := map[string][]Warning{}
	loaded, err := configtree.Load(path, readFile, func(origin string, node *yaml.Node) (*yaml.Node, error) {
		normalized, warnings, err := normalizeExtendedConfig(origin, node, absPath(path) != origin, additional...)
		warningsByFile[origin] = warnings
		return normalized, err
	})
	if err != nil {
		if _, ok := errors.AsType[*os.PathError](err); ok {
			return nil, fmt.Errorf("could not read config file %q: %w", path, err)
		}
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	inputs := map[*yaml.Node]Input{}
	if len(loaded.Files) > 1 {
		for node, file := range loaded.Origins {
			inputs[node] = Input{File: file}
		}
	}
	resolved, err := ResolveNode(loaded.Node, inputs, additional...)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	resolved.Config.filename = absPath(path)
	resolved.Config.configFiles = loaded.Files
	resolved.Warnings = nil
	for _, file := range loaded.Files {
		resolved.Warnings = MergeWarnings(resolved.Warnings, warningsByFile[file])
	}
	return &Loaded{resolved, path}, nil
}

// RepositoryConfigNames lists the config file names read from .github, in precedence order.
var RepositoryConfigNames = []string{"actionlint.yaml", "actionlint.yml", "actionlint.json", "actionlint.toml"}

// LoadRepository reads the first existing file named in RepositoryConfigNames
// from the repository's .github directory.
func LoadRepository(root string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, string, error) {
	for _, f := range RepositoryConfigNames {
		p := filepath.Join(root, ".github", f)
		c, err := Read(p, readFile, additional...)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return nil, "", fmt.Errorf("could not parse config file %q: %w", p, err)
		default:
			return c, p, nil
		}
	}
	return nil, "", nil
}

// Path returns the absolute path of the file this configuration was loaded from.
func (cfg *Config) Path() string {
	if cfg == nil {
		return ""
	}
	return cfg.filename
}

// Sources lists every file that contributed to this configuration, bases first.
func (cfg *Config) Sources() []string {
	if cfg == nil {
		return nil
	}
	return cfg.configFiles
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		path = p
	}
	return path
}
