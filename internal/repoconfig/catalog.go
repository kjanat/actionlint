package repoconfig

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/internal/lintconfig"
	"go.yaml.in/yaml/v4"
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
	Name            string
	Description     string
	Category        string
	Recommended     bool
	SupportsActions bool
	Scope           Scope
	Options         func() any
	Configure       func(*Config, Setting)
}

// Nursery reports whether the rule is excluded from stable presets.
func (r Rule) Nursery() bool { return r.Category == "nursery" }

// Rules lists the built-in checks in registration order.
func Rules() []Rule {
	return []Rule{
		{Name: "workflow-run-names", Description: "Report workflow_run references to unknown repository workflows", Category: "correctness", Recommended: true},
		{Name: "unsound-ternary", Description: "Report a && b || c expressions where b is always falsy", Category: "correctness", Recommended: true},
		{Name: "ruff", Description: "Analyze embedded Python scripts with Ruff", Category: "external", Recommended: true, SupportsActions: true, Scope: CompositeScope},
		{Name: "syntax-check", Description: "Validate GitHub Actions workflow syntax", Category: "correctness", Recommended: true, SupportsActions: true},
		{Name: "inline-suppression", Description: "Validate inline suppression directives", Category: "correctness", Recommended: true, Scope: SourceScope},
		{Name: "missing-local-action", Description: "Report local action references whose manifests cannot be found", Category: "correctness", Recommended: true},
		{Name: "string-conditions", Description: "Flag string conditions whose truthiness may be misleading", Category: "suspicious"},
		{Name: "mixed-type-comparisons", Description: "Flag condition comparisons that implicitly convert scalar types", Category: "suspicious"},
		{Name: "case-insensitive-conditions", Description: "Flag case-insensitive identity comparisons", Category: "suspicious"},
		{Name: "mixed-type-matrix-filters", Description: "Flag matrix filters that compare different or unknown scalar types", Category: "suspicious"},
		{Name: "disallow-suppressions", Description: "Report suppressions forbidden by policy", Category: "policy", Scope: SourceScope, Options: func() any { return &SuppressionsPolicy{} }, Configure: configureSuppressionsPolicy},
		{Name: "matrix", Description: "Validate matrix definitions, combinations, and filters", Category: "correctness", Recommended: true},
		{Name: "credentials", Description: "Detect hardcoded passwords in job and service containers", Category: "security", Recommended: true},
		{Name: "shell-name", Description: "Validate script shells for the selected runner", Category: "correctness", Recommended: true},
		{Name: "runner-label", Description: "Report unknown or incompatible runner labels", Category: "correctness", Recommended: true},
		{Name: "events", Description: "Validate workflow triggers and their options", Category: "correctness", Recommended: true},
		{Name: "job-needs", Description: "Report undefined job dependencies and dependency cycles", Category: "correctness", Recommended: true},
		{Name: "parallel-steps", Description: "Validate background-step references and restrictions within parallel groups", Category: "correctness", Recommended: true},
		{Name: "action", Description: "Validate action references, inputs, outputs, and local metadata", Category: "correctness", Recommended: true, SupportsActions: true},
		{Name: "executable-bit", Description: "Report directly invoked repository scripts not marked executable in Git", Category: "correctness", Recommended: true},
		{Name: "env-var", Description: "Validate environment variable declarations", Category: "correctness", Recommended: true},
		{Name: "id", Description: "Report invalid job or step IDs and duplicate step IDs", Category: "correctness", Recommended: true},
		{Name: "glob", Description: "Validate branch, tag, and path filter patterns", Category: "correctness", Recommended: true},
		{Name: "permissions", Description: "Validate token permission scopes and access levels", Category: "correctness", Recommended: true},
		{Name: "workflow-call", Description: "Validate reusable workflow references and their declared inputs and outputs", Category: "correctness", Recommended: true},
		{Name: "expression", Description: "Validate expression syntax, names, types, and context availability", Category: "correctness", Recommended: true},
		{Name: "deprecated-commands", Description: "Report deprecated set-output, save-state, set-env, and add-path commands", Category: "correctness", Recommended: true, SupportsActions: true, Scope: SourceScope},
		{Name: "if-cond", Description: "Report conditions that always evaluate to true or false", Category: "correctness", Recommended: true},
		{Name: "require-commit-hash", Description: "Require full commit SHAs for actions and workflows, and digests for images", Category: "policy", Options: func() any { return &lintconfig.CommitHashOptions{} }, Configure: boolRuleOption(func(p *Policy) **bool { return &p.RequireCommitHash })},
		{Name: "require-job-timeout", Description: "Require explicit job timeouts within configured bounds", Category: "policy", Options: func() any { return &JobTimeoutPolicy{} }, Configure: configureJobTimeout},
		{Name: "require-permissions", Description: "Require explicit token permissions at workflow or job scope", Category: "policy", Options: func() any { return &PermissionsPolicy{} }, Configure: configurePermissions},
		{Name: "required-actions", Description: "Require actions listed in the project policy", Category: "policy", Options: func() any { return &RequiredActionsOptions{} }, Configure: configureRequiredActions},
		{Name: "cache-write-untrusted", Description: "Report cache write access granted on low-trust triggers", Category: "security", Recommended: true, Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheWriteUntrusted })},
		{Name: "cache-call-unrestricted", Description: "Report reusable workflow calls without explicit cache limits on low-trust triggers", Category: "security", Recommended: true, Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheCallUnrestricted })},
		{Name: "cache-operation", Description: "Report cache actions blocked by explicit cache access limits", Category: "correctness", Recommended: true, Configure: boolRuleOption(func(p *Policy) **bool { return &p.CacheOperation })},
		{Name: "shellcheck", Description: "Analyze embedded shell scripts with ShellCheck", Category: "external", Recommended: true, SupportsActions: true, Scope: SourceScope},
	}
}

// FindRule returns the catalog entry with the given name.
func FindRule(name string) (Rule, bool) {
	for _, rule := range Rules() {
		if rule.Name == name {
			return rule, true
		}
	}
	return Rule{}, false
}

// Suppressible reports whether inline directives and ignore settings may name the rule.
func Suppressible(name string, registeredRules ...string) bool {
	_, ok := FindRule(name)
	return ok || slices.Contains(registeredRules, name)
}

func boolRuleOption(field func(*Policy) **bool) func(*Config, Setting) {
	return func(c *Config, s Setting) { *field(&c.Policy) = new(s.Level != "off") }
}

func configureJobTimeout(c *Config, s Setting) {
	if s.Level == "off" {
		c.Policy.RequireJobTimeout = nil
		return
	}
	if options, ok := s.Options.(*JobTimeoutPolicy); ok {
		c.Policy.RequireJobTimeout = options
	} else if !c.Policy.RequireJobTimeout.Enabled() {
		c.Policy.RequireJobTimeout = RequireJobTimeout(0)
	}
}

func configurePermissions(c *Config, s Setting) {
	if s.Level == "off" {
		c.Policy.RequirePermissions = nil
		return
	}
	if options, ok := s.Options.(*PermissionsPolicy); ok {
		c.Policy.RequirePermissions = options
	} else if !c.Policy.RequirePermissions.Enabled() {
		c.Policy.RequirePermissions, _ = RequirePermissions("workflow")
	}
}

func configureSuppressionsPolicy(c *Config, s Setting) {
	if s.Level == "off" {
		c.Policy.DisallowSuppressions = nil
		return
	}
	if options, ok := s.Options.(*SuppressionsPolicy); ok {
		c.Policy.DisallowSuppressions = options
	} else if !c.Policy.DisallowSuppressions.Enabled() {
		c.Policy.DisallowSuppressions, _ = DisallowSuppressions("all")
	}
}

func configureRequiredActions(c *Config, s Setting) {
	if s.Level == "off" {
		c.Policy.RequiredActions = nil
		return
	}
	if options, ok := s.Options.(*RequiredActionsOptions); ok {
		c.Policy.RequiredActions = options.Actions
	}
}
