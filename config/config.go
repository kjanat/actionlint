// Package config loads and describes actionlint configuration.
package config

import "actionlint.kjanat.dev/internal/engine"

type Config = engine.Config
type Inspection = engine.ConfigInspection
type Origin = engine.ConfigOrigin
type Overlay = engine.ConfigOverlay
type ConfigOverride = engine.ConfigOverride
type Report = engine.ConfigReport
type Selection = engine.ConfigSelection
type Warning = engine.ConfigWarning
type DefaultPermissionsAssumption = engine.DefaultPermissionsAssumption
type FilesConfig = engine.FilesConfig
type IgnoreConfig = engine.IgnoreConfig
type IgnoreConfigs = engine.IgnoreConfigs
type JobTimeoutPolicy = engine.JobTimeoutPolicy
type LintConfig = engine.LintConfig
type LintRulesConfig = engine.LintRulesConfig
type LocalActionSources = engine.LocalActionSources
type PathConfig = engine.PathConfig
type PermissionsPolicy = engine.PermissionsPolicy
type Policy = engine.Policy
type RuffToolConfig = engine.RuffToolConfig
type RuleGroupConfig = engine.RuleGroupConfig
type RuleLevel = engine.RuleLevel
type RulePreset = engine.RulePreset
type Presets = engine.RulePresets
type RuleSetting = engine.RuleSetting
type SelfHostedRunnerConfig = engine.SelfHostedRunnerConfig
type ShellcheckConfig = engine.ShellcheckConfig
type ShellcheckConfigSource = engine.ShellcheckConfigSource
type ShellcheckToolConfig = engine.ShellcheckToolConfig
type SuppressionsPolicy = engine.SuppressionsPolicy
type ToolsConfig = engine.ToolsConfig

const (
	DefaultPermissionsAssumptionUnset      = engine.DefaultPermissionsAssumptionUnset
	DefaultPermissionsAssumptionRestricted = engine.DefaultPermissionsAssumptionRestricted
	DefaultPermissionsAssumptionPermissive = engine.DefaultPermissionsAssumptionPermissive
)

// Parse decodes a standalone configuration document. Use Load to resolve extends.
func Parse(source []byte) (*Config, error) { return engine.ParseConfig(source) }

// Load reads a configuration file and resolves its inheritance.
func Load(path string) (*Config, error) { return engine.ReadConfigFile(path) }

// Inspect reports effective configuration and optionally its origins.
func Inspect(selection Selection, origins bool) (Inspection, error) {
	return engine.InspectConfig(selection, origins)
}

func RequireJobTimeout(maxMinutes float64) *JobTimeoutPolicy {
	return engine.RequireJobTimeout(maxMinutes)
}
func RequireJobTimeoutRange(minMinutes, maxMinutes float64) (*JobTimeoutPolicy, error) {
	return engine.RequireJobTimeoutRange(minMinutes, maxMinutes)
}
func RequirePermissions(scope string) (*PermissionsPolicy, error) {
	return engine.RequirePermissions(scope)
}
