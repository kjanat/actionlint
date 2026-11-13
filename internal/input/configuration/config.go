// Package config owns the repository configuration document: its model, decoding,
// validation, inheritance, overlays and inspection.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	configaddress "actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"

	"github.com/bmatcuk/doublestar/v4"
)

// IgnorePatterns contains regular expressions that match diagnostic messages.
type IgnorePatterns []*regexp.Regexp

// Match reports whether a pattern matches the diagnostic message.
func (pats IgnorePatterns) Match(message string) bool {
	for _, r := range pats {
		if r.MatchString(message) {
			return true
		}
	}
	return false
}

// DecodeConfig validates the shared configuration value.
func (pats *IgnorePatterns) DecodeConfig(value any) error {
	if value == nil {
		*pats = nil
		return nil
	}
	patterns, ok := value.([]any)
	if !ok {
		return errors.New("ignore patterns must be an array")
	}
	next := make(IgnorePatterns, 0, len(patterns))
	for i, value := range patterns {
		var pattern string
		switch scalar := value.(type) {
		case string:
			pattern = scalar
		case bool:
			pattern = strconv.FormatBool(scalar)
		case json.Number:
			pattern = string(scalar)
		case nil:
			pattern = "null"
		default:
			pattern = fmt.Sprint(scalar)
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return binding.At(strconv.Itoa(i), fmt.Errorf("invalid regular expression %q: %w", pattern, err))
		}
		next = append(next, compiled)
	}
	*pats = next
	return nil
}

// PathConfig holds settings for one path pattern in the configuration file's "paths" mapping.
type PathConfig struct {
	// Ignore suppresses diagnostics whose message matches any of these Go regular expressions.
	//
	// Applies only to workflow files matching the parent path glob. For example,
	// `["shellcheck reported issue in this script: SC2086"]` ignores that ShellCheck diagnostic.
	// Omit this key, use `null`, or use `[]` to suppress nothing. The `-ignore` CLI option also matches messages.
	Ignore IgnorePatterns `yaml:"ignore" jsonschema:"nullable"`
}

// DecodeConfig preserves ignored legacy path keys while binding known settings.
func (p *PathConfig) DecodeConfig(value any) error {
	type plain PathConfig
	var next plain
	if err := binding.DecodeKnown(value, &next); err != nil {
		return err
	}
	*p = PathConfig(next)
	return nil
}

// Policy configures checks for cache safety and repository conventions.
// Cache checks default to enabled; repository conventions are opt-in.
// Omission and null select the default.
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
	// as well as the violations it would suppress. Omission, null, or false permits exceptions.
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
	// RequireActionMetadata requires usable fetched metadata for remote action
	// references. Disabled by default; this does not enable network access.
	RequireActionMetadata *bool `yaml:"require-action-metadata" jsonschema:"nullable"`
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
}

// DecodeConfig validates the shared configuration value.
func (p *policyWire) DecodeConfig(value any) error {
	type plain policyWire
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	*p = policyWire(next)
	return nil
}

// JobTimeoutPolicy holds the "require-job-timeout" policy. Its configuration value
// is a boolean, or a mapping that enables the check and sets "min-minutes" and "max-minutes".
type JobTimeoutPolicy struct {
	enabled    bool
	minMinutes float64
	maxMinutes float64
}

// RequireJobTimeout enables the check with the given upper limit in minutes.
// A value of zero or less sets no upper limit.
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

// DecodeConfig validates the shared configuration value.
func (p *JobTimeoutPolicy) DecodeConfig(value any) error {
	var next JobTimeoutPolicy
	switch selected := value.(type) {
	case bool:
		next.enabled = selected
	case map[string]any:
		next.enabled = true
		for key, value := range selected {
			if key != "min-minutes" && key != "max-minutes" {
				return binding.At(key, fmt.Errorf("unknown key %q in job timeout policy: %w", key, binding.ErrUnknownKey))
			}
			var bound float64
			if value == nil {
				return binding.At(key, errors.New("job timeout bound must be a number"))
			}
			var err error
			if scalar, ok := value.(string); ok {
				bound, err = strconv.ParseFloat(scalar, 64)
			} else {
				err = binding.Decode(value, &bound)
			}
			if err != nil {
				return binding.At(key, errors.New("job timeout bound must be a number"))
			}
			if math.IsNaN(bound) || math.IsInf(bound, 0) {
				return binding.At(key, errors.New("job timeout bound must be finite"))
			}
			if bound <= 0 {
				return binding.At(key, errors.New("job timeout bound must be greater than zero"))
			}
			if key == "min-minutes" {
				next.minMinutes = bound
			} else {
				next.maxMinutes = bound
			}
		}
		if next.maxMinutes > 0 && next.minMinutes > next.maxMinutes {
			return binding.At("min-minutes", errors.New("min-minutes must not exceed max-minutes"))
		}
	default:
		return errors.New("require-job-timeout must be a boolean or a mapping")
	}
	*p = next
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

// DecodeConfig validates the shared configuration value.
func (p *PermissionsPolicy) DecodeConfig(value any) error {
	next := PermissionsPolicy{scope: "workflow"}
	switch selected := value.(type) {
	case bool:
		next.enabled = selected
	case map[string]any:
		next.enabled = true
		for key, value := range selected {
			if key != "scope" {
				return binding.At(key, fmt.Errorf("unknown permissions policy key %q: %w", key, binding.ErrUnknownKey))
			}
			scope, ok := value.(string)
			if !ok || (scope != "workflow" && scope != "job") {
				return binding.At(key, errors.New("scope must be workflow or job"))
			}
			next.scope = scope
		}
	default:
		return errors.New("require-permissions must be a boolean or a mapping")
	}
	*p = next
	return nil
}

// Config configures validation of GitHub Actions workflows for this repository.
//
// Declare custom runner labels and available variable or secret names, suppress selected
// diagnostics by file path, choose assumed token permissions, and enable repository policy checks.
// Create a configuration with `actionlint config init`, or select a file with `--config`.
// Every setting is optional; normal workflow correctness checks run without a configuration file.
type Config struct {
	// Extends loads local or HTTPS configuration files in order.
	// Relative references resolve against their declaring file or URL. Later
	// bases override earlier bases; this file wins over all bases.
	Extends []string `yaml:"extends,omitempty" jsonschema:"nullable"`
	// Plugins declares canonical plugin packages with exact registry versions,
	// or digest-pinned HTTPS/OCI artifacts. Install/update resolves aliases and
	// latest before saving. Use name, version and registry for a custom catalogue,
	// or source for a direct artifact. Empty or null disables plugins.
	Plugins PluginDeclarations `yaml:"plugins,omitempty" jsonschema:"nullable"`
	// Files selects repository-relative inputs shared by analysis features.
	Files FilesConfig `yaml:"files,omitempty" jsonschema:"nullable"`
	// Output configures defaults for CLI reporting.
	Output OutputConfig `yaml:"output,omitempty" jsonschema:"nullable"`
	// Network controls fallback services for remote metadata.
	Network NetworkConfig `yaml:"network,omitempty" jsonschema:"nullable"`
	// Lint configures rule selection separately from repository policy.
	Lint LintConfig `yaml:"lint" jsonschema:"nullable"`
	// Repository supplies overrides for repository knowledge obtained by the host.
	Repository RepositoryConfig `yaml:"repository" jsonschema:"nullable"`
	// Overrides applies per-file lint settings in declaration order. Later matching
	// settings win; mappings merge and arrays replace. Paths are repository-relative.
	Overrides []Override `yaml:"overrides" jsonschema:"nullable"`
	// Ignores suppresses a rule for matching files, actions, jobs, or steps.
	// Every specified selector must match. Later config files replace the list.
	Ignores IgnoreConfigs `yaml:"ignores" jsonschema:"nullable"`
	// Tools configures external linters for both the CLI and GitHub Action, keyed
	// by the rule name of each tool provider, such as ruff or shellcheck.
	Tools ToolsConfig `yaml:"tools" jsonschema:"nullable"`
	// LocalActions maps generated action directories to checked-in source directories.
	// Both paths are relative to the repository root, using ./ or $/ prefixes.
	// Known checkout placements are removed before matching. Sources resolve once
	// to actual directories; mappings do not chain.
	LocalActions LocalActionSources `yaml:"local-actions" jsonschema:"nullable"`
	// Paths applies configuration to workflow files matching a glob pattern.
	//
	// Keys are paths relative to the repository root, using `/` separators and doublestar glob syntax,
	// for example `.github/workflows/**/*.yaml`. All matching entries apply. Each entry can set `ignore`.
	Paths map[string]PathConfig `yaml:"-"`
	// Policy configures cache safety checks and repository conventions, such as pinned actions and job timeouts.
	//
	// Cache policies default to true; other policies are opt-in. Set individual keys to override their
	// defaults. Omit the mapping or use `{}` or `null` to keep defaults. Syntax checks always run.
	Policy Policy `yaml:"-"`
}

// PathConfigs returns settings matching a path relative to the project root.
func (cfg *Config) PathConfigs(path string) []PathConfig {
	path = filepath.ToSlash(path)

	var ret []PathConfig
	if cfg != nil {
		for p, c := range cfg.Paths {
			// Glob patterns were validated in `Parse()`
			if doublestar.MatchUnvalidated(p, path) {
				patterns := slices.Clone(c.Ignore)
				for i, pattern := range c.Ignore {
					if pattern != nil {
						patterns[i] = new(*pattern)
					}
				}
				ret = append(ret, PathConfig{Ignore: patterns})
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
	if cfg == nil || cfg.Policy.RequireJobTimeout == nil {
		return nil
	}
	return new(*cfg.Policy.RequireJobTimeout)
}

// RequiresPermissions returns the "require-permissions" policy, or nil for an unset key or nil receiver.
func (cfg *Config) RequiresPermissions() *PermissionsPolicy {
	if cfg == nil || cfg.Policy.RequirePermissions == nil {
		return nil
	}
	return new(*cfg.Policy.RequirePermissions)
}

// Parse decodes and validates an actionlint configuration document. It returns
// an error if decoding or validation fails, or if the document uses extends.
func (ruleSet RuleSet) Parse(b []byte, additional ...string) (*Config, error) {
	resolved, err := ruleSet.ResolveDocument(b, additional...)
	if err != nil {
		return nil, err
	}
	if len(resolved.Config.Extends) > 0 {
		return nil, errors.New("extends requires a file path; load the configuration file instead of parsing bytes")
	}
	return resolved.Config, nil
}

// Resolved keeps validated values and their provenance from the same document.
type Resolved struct {
	Provenance Provenance
	Value      any
	Inputs     map[string]Input
	Config     *Config
	Values     map[string]any
	Origins    map[string]Origin
	Warnings   []Warning
}

func (ruleSet RuleSet) ResolveDocument(b []byte, additional ...string) (Resolved, error) {
	doc, err := document.Parse("", b)
	if err != nil {
		return Resolved{}, decodeError{cause: err}
	}
	return ruleSet.ResolveValue(doc.Value, documentInputs(doc), additional...)
}

func documentInputs(doc document.Document) map[string]Input {
	inputs := map[string]Input{}
	for path, location := range doc.ValueLocations {
		key := doc.Locations[path]
		inputs[path] = Input{File: location.File, Line: location.Line, Column: location.Column, KeyLine: key.Line, KeyColumn: key.Column}
	}
	return inputs
}

func (ruleSet RuleSet) ResolveValue(root any, inputs map[string]Input, additional ...string) (Resolved, error) {
	return ruleSet.resolveValue(root, inputs, true, additional...)
}

func (ruleSet RuleSet) resolveValue(root any, inputs map[string]Input, bindRules bool, additional ...string) (Resolved, error) {
	inputs = ruleSet.migrationInputs(root, inputs)
	root = ruleSet.normalizeToolSwitch(root)
	legacyWarnings := configWarnings(root, inputs)
	var err error
	root, _, err = ruleSet.migrateNode(root)
	if err != nil {
		return Resolved{}, sourceError(err, inputs)
	}

	var c Config
	if root != nil {
		if err := ruleSet.decodeValue(root, &c, bindRules); err != nil {
			return Resolved{}, decodeError{cause: sourceError(err, inputs)}
		}
		if plugins := field(root, "plugins"); plugins != nil && isNull(plugins) {
			c.Plugins = PluginDeclarations{}
		}
	}
	ruleSet.projectSettings(&c)
	if bindRules {
		if err := ruleSet.ValidateRuleNames(&c, additional...); err != nil {
			return Resolved{}, sourceError(err, inputs)
		}
	}
	for pat := range c.Paths {
		if !doublestar.ValidatePattern(pat) {
			return Resolved{}, fmt.Errorf("invalid glob pattern %q in paths", pat)
		}
	}
	values, err := ruleSet.EffectiveValues(&c)
	if err != nil {
		return Resolved{}, err
	}
	origins := map[string]Origin{}
	configDefaultOrigins(values, "", origins)
	if err := configOrigins(root, "", origins, inputs); err != nil {
		return Resolved{}, err
	}
	for name, tool := range c.Tools {
		if tool.Config != nil {
			tool.Config.fromInput = origins["/tools/"+configPointerPart(name)+"/config"].Source == "input"
		}
	}
	for i := range c.Overrides {
		if tools := c.Overrides[i].Tools; tools != nil {
			for _, tool := range *tools {
				if tool.Config != nil {
					tool.Config.fromInput = origins["/overrides"].Source == "input"
				}
			}
		}
	}
	c.Lint.Rules.bindingSources = ruleBindingSources("", &c, inputs)
	return Resolved{Value: root, Inputs: inputs, Config: &c, Values: values, Origins: origins, Warnings: MergeWarnings(legacyWarnings, configWarnings(root, inputs))}, nil
}

// Load reads settings from a configuration file. Read also retains source provenance.
func (ruleSet RuleSet) Load(path string, additional ...string) (*Config, error) {
	return ruleSet.LoadContext(context.Background(), path, additional...)
}

// LoadContext reads settings with cancellable local or HTTPS inheritance.
// ReadContext also retains source provenance for analysis and path interpolation.
func (ruleSet RuleSet) LoadContext(ctx context.Context, path string, additional ...string) (*Config, error) {
	source, err := ruleSet.ReadContext(ctx, path, nil, additional...)
	if err != nil {
		return nil, err
	}
	return source.Config, nil
}

func (ruleSet RuleSet) Read(path string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, error) {
	return ruleSet.ReadContext(context.Background(), path, readFile, additional...)
}

// ReadContext preserves a supplied source reader for local and HTTPS inputs.
// Without one, the host reader acquires local files and explicit HTTPS sources.
func (ruleSet RuleSet) ReadContext(ctx context.Context, path string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, error) {
	return ruleSet.readContext(ctx, path, readFile, true, additional...)
}

// ReadBeforePluginsContext loads inherited configuration before plugin rule descriptors are available.
func (ruleSet RuleSet) ReadBeforePluginsContext(ctx context.Context, path string, readFile func(string) ([]byte, error)) (*Loaded, error) {
	return ruleSet.readContext(ctx, path, readFile, false)
}

func (ruleSet RuleSet) readContext(ctx context.Context, path string, readFile func(string) ([]byte, error), bindRules bool, additional ...string) (*Loaded, error) {
	reader := configsource.New(ctx, readFile)
	warningsByFile := map[string][]Warning{}
	var bindingSources []bindingSource
	documents := map[string]any{}
	loaded, err := loadConfigTree(path, reader.ReadConfig, func(origin string, doc document.Document) (document.Document, error) {
		inputs := ruleSet.migrationInputs(doc.Value, documentInputs(doc))
		normalized, warnings, err := ruleSet.normalizeExtendedConfig(origin, doc.Value, inputs, absPath(path) != origin, bindRules, func(value any) {
			documents[origin] = captureRuleDocument(value)
		}, additional...)
		warningsByFile[origin] = warnings
		if err != nil {
			return doc, err
		}
		var settings Config
		if err := ruleSet.decodeValue(normalized, &settings, bindRules); err != nil {
			return doc, err
		}
		bindingSources = append(bindingSources, ruleBindingSources(origin, &settings, inputs)...)
		doc.Value = normalized
		for pointer, input := range inputs {
			location := document.Location{File: input.File, Line: input.Line, Column: input.Column}
			if _, ok := doc.Locations[pointer]; !ok {
				doc.Locations[pointer] = document.Location{File: input.File, Line: input.KeyLine, Column: input.KeyColumn}
			}
			if _, ok := doc.ValueLocations[pointer]; !ok {
				doc.ValueLocations[pointer] = location
			}
		}
		return doc, nil
	})
	if err != nil {
		if _, ok := errors.AsType[*os.PathError](err); ok {
			return nil, fmt.Errorf("could not read config file %q: %w", path, err)
		}
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	resolved, err := ruleSet.resolveValue(loaded.Value, loaded.Origins, bindRules, additional...)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	resolved.Provenance = Provenance{path: absPath(path), files: slices.Clone(loaded.Files)}
	resolved.Config.Lint.Rules.bindingSources = bindingSources
	resolved.Config.Lint.Rules.sources = &ruleSources{root: loaded.Files[len(loaded.Files)-1], documents: documents}
	resolved.Warnings = nil
	for _, file := range loaded.Files {
		resolved.Warnings = MergeWarnings(resolved.Warnings, warningsByFile[file])
	}
	return &Loaded{resolved, path}, nil
}

// RepositoryConfigNames lists the config file names read from .github, in precedence order.
var RepositoryConfigNames = func() []string {
	names := make([]string, 0, len(FileFormats()))
	for _, format := range FileFormats() {
		names = append(names, format.Filename())
	}
	return names
}()

// LoadRepository reads the first existing file named in RepositoryConfigNames
// from the repository's .github directory.
func (ruleSet RuleSet) LoadRepository(root string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, string, error) {
	return ruleSet.LoadRepositoryContext(context.Background(), root, readFile, additional...)
}

// LoadRepositoryContext discovers configuration with cancellable inheritance.
func (ruleSet RuleSet) LoadRepositoryContext(ctx context.Context, root string, readFile func(string) ([]byte, error), additional ...string) (*Loaded, string, error) {
	for _, f := range RepositoryConfigNames {
		p := filepath.Join(root, ".github", f)
		c, err := ruleSet.ReadContext(ctx, p, readFile, additional...)
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

func absPath(path string) string {
	if configaddress.Remote(path) {
		return path
	}
	if p, err := filepath.Abs(path); err == nil {
		path = p
	}
	return path
}
