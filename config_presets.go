package actionlint

// RulePresets enables checks without a configuration file. These runtime
// selections apply after file overrides and never mutate project configuration.
type RulePresets struct {
	// Strict enables all stable rules, including non-recommended rules.
	// Configured bounds, scopes, report modes and diagnostic suppressions remain.
	// RequiredActions still needs a configured list of required actions.
	Strict bool
	// Experimental selects nursery rules. Nil retains configuration; false
	// disables nursery rules; true respects explicit rule and group settings.
	Experimental *bool
}

func (p RulePresets) apply(config *Config) *Config {
	next := Config{}
	if config != nil {
		next = *config
	}
	next.Lint.Rules.resolved = make(map[string]RuleLevel)
	for _, d := range builtinRuleDescriptors() {
		setting := config.ruleSetting(d, p)
		next.Lint.Rules.resolved[d.Name] = setting.Level
		if d.configure != nil {
			d.configure(&next, setting)
		}
	}
	return &next
}
