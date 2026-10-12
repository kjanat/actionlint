package actionlint

import (
	"fmt"

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

// configureRuleDescriptor keeps adapters for legacy policy settings in the same
// catalog used for discovery, schema generation, presets and rule selection.
func configureRuleDescriptor(d *ruleDescriptor) {
	switch d.Name {
	case "credentials":
		d.Category = "security"
	case "cache-write-untrusted":
		d.Category = "security"
		d.configure = boolRuleOption(func(p *Policy) **bool { return &p.CacheWriteUntrusted })
	case "cache-call-unrestricted":
		d.Category = "security"
		d.configure = boolRuleOption(func(p *Policy) **bool { return &p.CacheCallUnrestricted })
	case "cache-operation":
		d.Category = "correctness"
		d.configure = boolRuleOption(func(p *Policy) **bool { return &p.CacheOperation })
	case "require-commit-hash":
		d.configure = boolRuleOption(func(p *Policy) **bool { return &p.RequireCommitHash })
	case "require-job-timeout":
		d.newOptions = func() any { return &JobTimeoutPolicy{} }
		d.configure = func(c *Config, s RuleSetting) {
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
	case "require-permissions":
		d.newOptions = func() any { return &PermissionsPolicy{} }
		d.configure = func(c *Config, s RuleSetting) {
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
	case "disallow-suppressions":
		d.enabled = func(c ruleContext) bool { return c.config != nil && c.config.Policy.DisallowSuppressions.Enabled() }
		d.newOptions = func() any { return &SuppressionsPolicy{} }
		d.configure = func(c *Config, s RuleSetting) {
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
	case "required-actions":
		d.newOptions = func() any { return &RequiredActionsOptions{} }
		d.configure = func(c *Config, s RuleSetting) {
			if s.Level == "off" {
				c.Policy.RequiredActions = nil
				return
			}
			if options, ok := s.Options.(*RequiredActionsOptions); ok {
				c.Policy.RequiredActions = options.Actions
			}
		}
	}
}

func boolRuleOption(field func(*Policy) **bool) func(*Config, RuleSetting) {
	return func(c *Config, s RuleSetting) { *field(&c.Policy) = new(s.Level != "off") }
}
