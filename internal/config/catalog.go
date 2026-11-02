package config

import (
	"slices"
	"sync"

	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"
)

// Scope selects which document's configuration a rule reads.
type Scope uint8

const (
	CallerScope Scope = iota
	SourceScope
	CompositeScope
)

// Rule is the configuration vocabulary of one built-in check: identity, category,
// default selection, option type and policy projection. Execution stays with the host.
type Rule struct {
	rule.Descriptor
	Group           string
	SupportsActions bool
	Scope           Scope
	// DefaultLevel is the recommended severity; omission means on.
	DefaultLevel Level
	Options      func() any
	Configure    func(*Config, Setting)
}

// Nursery reports whether the rule is excluded from stable presets.
func (r Rule) Nursery() bool { return r.Maturity == "nursery" }

// Rules lists the built-in checks in registration order, then the bundled
// tool providers' rules.
func Rules() []Rule {
	rules := slices.Clone(registeredRules())
	for i := range rules {
		rules[i].Descriptor = rules[i].Descriptor.Clone()
	}
	return rules
}

// Registration and metadata depend only on embedded build inputs. Cache them
// privately; public lookups detach descriptors before exposing their slices.
var cachedRules struct {
	once  sync.Once
	rules []Rule
}

func registeredRules() []Rule {
	cachedRules.once.Do(func() { cachedRules.rules = buildRegisteredRules() })
	return cachedRules.rules
}

func buildRegisteredRules() []Rule {
	rules := slices.Concat([]Rule{
		{Name: "workflow-run-names"},
		{Name: "unsound-ternary"},
		{Name: "syntax-check", SupportsActions: true},
		{Name: "inline-suppression", Scope: SourceScope},
		{Name: "unused-suppression", SupportsActions: true, Scope: SourceScope, DefaultLevel: "info"},
		{Name: "missing-local-action"},
		{Name: "string-conditions"},
		{Name: "mixed-type-comparisons"},
		{Name: "case-insensitive-conditions"},
		{Name: "mixed-type-matrix-filters"},
		{Name: "disallow-suppressions", Scope: SourceScope, Options: func() any { return &SuppressionsPolicy{} }, Configure: configureSuppressionsPolicy},
		{Name: "matrix"},
		{Name: "credentials"},
		{Name: "shell-name"},
		{Name: "runner-label"},
		{Name: "events"},
		{Name: "job-needs"},
		{Name: "parallel-steps"},
		{Name: "action", SupportsActions: true},
		{Name: "executable-bit"},
		{Name: "env-var"},
		{Name: "id"},
		{Name: "glob"},
		{Name: "permissions"},
		{Name: "missing-workflow-permissions", DefaultLevel: "warn"},
		{Name: "workflow-call"},
		{Name: "expression", SupportsActions: true},
		{Name: "expression-syntax", SupportsActions: true},
		{Name: "expression-reference", SupportsActions: true},
		{Name: "context-availability", SupportsActions: true},
		{Name: "expression-type", SupportsActions: true},
		{Name: "expression-call", SupportsActions: true},
		{Name: "event-payload", SupportsActions: true},
		{Name: "script-injection", SupportsActions: true},
		{Name: "script-interpolation", SupportsActions: true},

		{Name: "deprecated-commands", SupportsActions: true, Scope: SourceScope},
		{Name: "if-cond"},
		{Name: "numeric-values"},
		{Name: "empty-triggers"},
		{Name: "empty-job"},
		{Name: "empty-run"},
		{Name: "require-commit-hash", Options: func() any { return &lintconfig.CommitHashOptions{} }, Configure: boolRuleOption(func(p *Policy) **bool { return &p.RequireCommitHash })},
		{Name: "require-action-metadata", Configure: boolRuleOption(func(p *Policy) **bool { return &p.RequireActionMetadata })},
		{Name: "require-job-timeout", Options: func() any { return &JobTimeoutPolicy{} }, Configure: configureJobTimeout},
		{Name: "require-permissions", Options: func() any { return &PermissionsPolicy{} }, Configure: configurePermissions},
		{Name: "cache-write-untrusted", Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheWriteUntrusted })},
		{Name: "cache-call-unrestricted", Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheCallUnrestricted })},
		{Name: "cache-operation", Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheOperation })},
	}, providerRules())
	for i := range rules {
		metadata, ok := rulecatalog.Find(rules[i].Name)
		if !ok {
			panic("missing rule metadata: " + rules[i].Name)
		}
		rules[i].Descriptor = metadata
		rules[i].Group = rulecatalog.ConfigGroup(metadata.Name)
	}
	return rules
}

// providerRules lists the bundled tool providers' rules. Providers check
// scripts in workflows and actions alike; their manifests choose which
// document's configuration applies.
func providerRules() []Rule {
	names := toolProviderNames()
	rules := make([]Rule, 0, len(names))
	for _, name := range names {
		rules = append(rules, Rule{Name: name, SupportsActions: true, Scope: toolProviders()[name].scope})
	}
	return rules
}

// FindRule returns the catalogue entry for name.
func FindRule(name string) (Rule, bool) {
	for _, rule := range registeredRules() {
		if rule.Name == name {
			rule.Descriptor = rule.Descriptor.Clone()
			return rule, true
		}
	}
	return Rule{}, false
}

// Suppressible reports whether inline directives and ignore settings may name the rule.
func Suppressible(name string, additional ...string) bool {
	_, ok := FindRule(name)
	return ok || slices.Contains(additional, name)
}

func boolRuleOption(field func(*Policy) **bool) func(*Config, Setting) {
	return func(c *Config, s Setting) { *field(&c.Policy) = new(s.Level() != "off") }
}

func configureJobTimeout(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.RequireJobTimeout = nil
		return
	}
	if options := new(JobTimeoutPolicy); loadOptions(s, options) {
		c.Policy.RequireJobTimeout = options
	} else if !c.Policy.RequireJobTimeout.Enabled() {
		c.Policy.RequireJobTimeout = RequireJobTimeout(0)
	}
}

func configurePermissions(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.RequirePermissions = nil
		return
	}
	if options := new(PermissionsPolicy); loadOptions(s, options) {
		c.Policy.RequirePermissions = options
	} else if !c.Policy.RequirePermissions.Enabled() {
		c.Policy.RequirePermissions, _ = RequirePermissions("workflow")
	}
}

func configureSuppressionsPolicy(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.DisallowSuppressions = nil
		return
	}
	if options := new(SuppressionsPolicy); loadOptions(s, options) {
		c.Policy.DisallowSuppressions = options
	} else if !c.Policy.DisallowSuppressions.Enabled() {
		c.Policy.DisallowSuppressions, _ = DisallowSuppressions("all")
	}
}
