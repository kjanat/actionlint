// Package config loads and describes actionlint configuration.
package config

import (
	configreport "actionlint.kjanat.dev/v2/config/report"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	lintconfig "actionlint.kjanat.dev/v2/internal/config/rules"
	"actionlint.kjanat.dev/v2/internal/pluginref"
)

type (
	Config     = repoconfig.Config
	Inspection = configreport.Inspection
	Origin     = configreport.Origin
	Overlay    = repoconfig.Overlay
	// OverlayError identifies individually valid inputs whose merged configuration is invalid.
	OverlayError                 = repoconfig.OverlayError
	ConfigOverride               = repoconfig.Override
	Report                       = configreport.Report
	Selection                    = repoconfig.Selection
	Warning                      = configreport.Warning
	DefaultPermissionsAssumption = repoconfig.DefaultPermissionsAssumption
	FilesConfig                  = repoconfig.FilesConfig
	IgnoreConfig                 = repoconfig.IgnoreConfig
	IgnoreConfigs                = repoconfig.IgnoreConfigs
	JobTimeoutPolicy             = repoconfig.JobTimeoutPolicy
	LintConfig                   = repoconfig.LintConfig
	LintRulesConfig              = repoconfig.LintRulesConfig
	LocalActionSources           = repoconfig.LocalActionSources
	PathConfig                   = repoconfig.PathConfig
	PluginDeclarations           = repoconfig.PluginDeclarations
	PluginReference              = pluginref.Reference
	PermissionsPolicy            = repoconfig.PermissionsPolicy
	Policy                       = repoconfig.Policy
	RuleGroupConfig              = repoconfig.Group
	RuleLevel                    = repoconfig.Level
	RulePreset                   = repoconfig.Preset
	Presets                      = repoconfig.Presets
	RuleSetting                  = repoconfig.Setting
	// RequiredActionsOptions selects action names or action/ref patterns required by a rule.
	RequiredActionsOptions = repoconfig.RequiredActionsOptions
	// CommitHashOptions selects exceptions to immutable action references.
	CommitHashOptions      = lintconfig.CommitHashOptions
	SelfHostedRunnerConfig = repoconfig.SelfHostedRunnerConfig
	SuppressionsPolicy     = repoconfig.SuppressionsPolicy
)

const (
	DefaultPermissionsAssumptionUnset      = repoconfig.DefaultPermissionsAssumptionUnset
	DefaultPermissionsAssumptionRestricted = repoconfig.DefaultPermissionsAssumptionRestricted
	DefaultPermissionsAssumptionPermissive = repoconfig.DefaultPermissionsAssumptionPermissive
)

// Parse decodes a standalone configuration document. Use Load to resolve extends.
// additionalRules lists registered analyzer names accepted in suppression settings.
func Parse(source []byte, additionalRules ...string) (*Config, error) {
	return repoconfig.Parse(source, additionalRules...)
}

// ParseOverlay validates a complete config document or one named configuration input.
// additionalRules lists registered analyzer names accepted in suppression settings.
func ParseOverlay(input string, source []byte, additionalRules ...string) (Overlay, error) {
	return repoconfig.ParseOverlay(input, source, additionalRules...)
}

// Load reads a configuration file and resolves its inheritance.
// additionalRules lists registered analyzer names accepted in suppression settings.
func Load(path string, additionalRules ...string) (*Config, error) {
	return repoconfig.Load(path, additionalRules...)
}

// Inspect reports effective configuration and optionally its origins.
// registeredRules adds caller-registered plugin IDs to the built-in rule catalog.
func Inspect(selection Selection, origins bool, registeredRules ...string) (Inspection, error) {
	return repoconfig.Inspect(selection, origins, registeredRules...)
}

func RequireJobTimeout(maxMinutes float64) *JobTimeoutPolicy {
	return repoconfig.RequireJobTimeout(maxMinutes)
}

func RequireJobTimeoutRange(minMinutes, maxMinutes float64) (*JobTimeoutPolicy, error) {
	return repoconfig.RequireJobTimeoutRange(minMinutes, maxMinutes)
}

func RequirePermissions(scope string) (*PermissionsPolicy, error) {
	return repoconfig.RequirePermissions(scope)
}

// DisallowSuppressions constructs an enabled suppression policy for the selected rules.
func DisallowSuppressions(report string, rules ...string) (*SuppressionsPolicy, error) {
	return repoconfig.DisallowSuppressions(report, rules...)
}
