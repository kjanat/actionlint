package configuration

import (
	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// Rule projects a declaration into configuration selection and legacy policy fields.
// Execution stays with the checking implementation.
type Rule struct {
	rulescope.Descriptor
	Group           string
	SupportsActions bool
	Scope           rulescope.ConfigurationScope
	Options         func() any
	// LegacyGroup, Policy, MovedOptions and MigrationNote describe how
	// configuration written for earlier versions selects the rule. See
	// [rulescope.Declaration].
	LegacyGroup   string
	Policy        string
	MovedOptions  []rulescope.MovedOption
	MigrationNote string
}

// policySetting projects a rule's resolved setting into the legacy policy
// field that the rule replaced.
type policySetting struct {
	// configure records the setting in the policy field.
	configure func(*Config, Setting)
	// enabled reports whether the policy field enables the rule. Nil leaves
	// the decision to the rule's level.
	enabled func(*Config) bool
	// enabledByDefault marks a policy that applies unless disabled.
	enabledByDefault bool
	// disable records an explicit off level after configure has run. Nil
	// means configure records it.
	disable func(*Config)
}

// policySettings binds each legacy policy key to its field in [Policy]. A rule
// names the key it replaced in [rulescope.Declaration.Policy].
var policySettings = map[string]policySetting{
	"cache-call-unrestricted": defaultPolicy(func(p *Policy) **bool { return &p.CacheCallUnrestricted }),
	"cache-operation":         defaultPolicy(func(p *Policy) **bool { return &p.CacheOperation }),
	"cache-write-untrusted":   defaultPolicy(func(p *Policy) **bool { return &p.CacheWriteUntrusted }),
	suppressionsPolicy: {
		configure: configureSuppressionsPolicy,
		enabled:   func(c *Config) bool { return c != nil && c.Policy.DisallowSuppressions.Enabled() },
		disable:   func(c *Config) { c.Policy.DisallowSuppressions = &SuppressionsPolicy{} },
	},
	"require-action-metadata": {configure: boolRuleOption(func(p *Policy) **bool { return &p.RequireActionMetadata })},
	"require-commit-hash": {
		configure: boolRuleOption(func(p *Policy) **bool { return &p.RequireCommitHash }),
		enabled:   (*Config).RequiresCommitHash,
	},
	"require-job-timeout": {
		configure: configureJobTimeout,
		enabled:   func(c *Config) bool { return c.RequiresJobTimeout().Enabled() },
		disable:   func(c *Config) { c.Policy.RequireJobTimeout = &JobTimeoutPolicy{} },
	},
}

// suppressionsPolicy is the policy key of the rule that reports forbidden
// suppressions. That rule cannot itself be suppressed, and its options name
// other rules.
const suppressionsPolicy = "disallow-suppressions"

// suppressionsRule returns the rule that replaced the suppressions policy.
func (ruleSet RuleSet) suppressionsRule() (Rule, bool) {
	for _, entry := range ruleSet.entries {
		if entry.Policy == suppressionsPolicy {
			return ruleSet.FindRule(entry.Name)
		}
	}
	return Rule{}, false
}

// policySetting returns the projection of the legacy policy the rule replaced.
func (r Rule) policySetting() (policySetting, bool) {
	setting, found := policySettings[r.Policy]
	return setting, found
}

// defaultPolicy projects a boolean policy that applies unless disabled.
func defaultPolicy(field func(*Policy) **bool) policySetting {
	return policySetting{
		configure: boolRuleOption(field),
		enabled: func(c *Config) bool {
			if c == nil {
				return true
			}
			enabled := *field(&c.Policy)
			return enabled == nil || *enabled
		},
		enabledByDefault: true,
	}
}

// CachePolicyEnabled reports whether the cache policy with this key applies.
// Cache policies apply unless disabled; other keys report true.
func (cfg *Config) CachePolicyEnabled(name string) bool {
	setting, found := policySettings[name]
	return !found || !setting.enabledByDefault || setting.enabled(cfg)
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

func configureSuppressionsPolicy(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.DisallowSuppressions = nil
		return
	}
	if options := new(SuppressionsPolicy); loadOptions(s, options) {
		options.names = c.Lint.Rules.ruleSet.Names()
		c.Policy.DisallowSuppressions = options
	} else if !c.Policy.DisallowSuppressions.Enabled() {
		c.Policy.DisallowSuppressions = &SuppressionsPolicy{report: ReportAll, names: c.Lint.Rules.ruleSet.Names()}
	}
}
