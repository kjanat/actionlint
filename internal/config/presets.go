package config

// Presets enables checks without a configuration file. These runtime
// selections apply after file overrides and never mutate project configuration.
type Presets struct {
	// Strict enables all stable rules, including non-recommended rules.
	// RequiredActions still needs a configured list of required actions.
	Strict bool
	// Experimental selects nursery rules. Nil uses configuration; false disables
	// nursery rules; true enables those not explicitly disabled by rule or group.
	Experimental *bool
}
