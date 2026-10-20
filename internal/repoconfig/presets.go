package repoconfig

// Presets enables checks without a configuration file. These runtime
// selections apply after file overrides and never mutate project configuration.
type Presets struct {
	// Strict enables all stable rules, including non-recommended rules.
	// Configured bounds, scopes, report modes and diagnostic suppressions remain.
	// RequiredActions still needs a configured list of required actions.
	Strict bool
	// Experimental selects nursery rules. Nil retains configuration; false
	// disables nursery rules; true respects explicit rule and group settings.
	Experimental *bool
}
