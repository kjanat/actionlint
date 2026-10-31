package config

import (
	"fmt"
	"slices"

	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"

	"github.com/kjanat/go-yaml/v4"
)

// RequiredActionsOptions configures exact actions or action/ref glob patterns.
// An empty selection requires nothing; presets never invent an action list.
type RequiredActionsOptions struct {
	// Actions contains action names or action/ref glob patterns required by this repository.
	Actions []string `yaml:"actions" jsonschema:"required"`
}

// UnmarshalYAML shares validation with the legacy policy setting.
func (o *RequiredActionsOptions) UnmarshalYAML(n *yaml.Node) error {
	var wire struct {
		Actions yaml.Node `yaml:"actions"`
	}
	if err := n.Load(&wire, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if wire.Actions.Kind != yaml.SequenceNode {
		return fmt.Errorf("yaml: required-actions options.actions must be a sequence at line:%d,col:%d", wire.Actions.Line, wire.Actions.Column)
	}
	return decodeRequiredActions(&wire.Actions, &o.Actions)
}

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
	Options         func() any
	Configure       func(*Config, Setting)
}

// Nursery reports whether the rule is excluded from stable presets.
func (r Rule) Nursery() bool { return r.Maturity == "nursery" }

// Rules lists the built-in checks in registration order, then the bundled
// tool providers' rules.
func Rules() []Rule {
	rules := slices.Concat([]Rule{
		{Name: "workflow-run-names"},
		{Name: "unsound-ternary"},
		{Name: "syntax-check", SupportsActions: true},
		{Name: "inline-suppression", Scope: SourceScope},
		{Name: "missing-local-action"},
		{Name: "string-conditions"},
		{Name: "mixed-type-comparisons"},
		{Name: "case-insensitive-conditions"},
		{Name: "mixed-type-matrix-filters"},
		{Name: "disallow-suppressions", Scope: SourceScope, Options: func() any { return &SuppressionsPolicy{} }, Configure: configureSuppressionsPolicy},
		{Name: "matrix"},
		{Name: "credentials"},
		{Name: "shell-name"},
		{Name: "runner-label", Options: func() any { return &RunnerLabelOptions{} }, Configure: configureRunnerLabels},
		{Name: "events"},
		{Name: "job-needs"},
		{Name: "parallel-steps"},
		{Name: "action", SupportsActions: true},
		{Name: "executable-bit"},
		{Name: "env-var"},
		{Name: "id"},
		{Name: "glob"},
		{Name: "permissions"},
		{Name: "workflow-call", Options: func() any { return &WorkflowCallOptions{} }, Configure: configureWorkflowCall},
		{Name: "expression", SupportsActions: true},
		{Name: "expression-syntax", SupportsActions: true},
		{Name: "expression-reference", SupportsActions: true, Options: func() any { return &ExpressionReferenceOptions{} }, Configure: configureExpressionReferences},
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
		{Name: "require-job-timeout", Options: func() any { return &JobTimeoutPolicy{} }, Configure: configureJobTimeout},
		{Name: "require-permissions", Options: func() any { return &PermissionsPolicy{} }, Configure: configurePermissions},
		{Name: "required-actions", Options: func() any { return &RequiredActionsOptions{} }, Configure: configureRequiredActions},
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
	for _, rule := range Rules() {
		if rule.Name == name {
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

func configureRequiredActions(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.RequiredActions = nil
		return
	}
	if options := new(RequiredActionsOptions); loadOptions(s, options) {
		c.Policy.RequiredActions = options.Actions
	}
}
