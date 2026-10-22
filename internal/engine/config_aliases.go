package engine

import "actionlint.kjanat.dev/internal/repoconfig"

type Config = repoconfig.Config
type ConfigInspection = repoconfig.Inspection
type ConfigOrigin = repoconfig.Origin
type ConfigOverlay = repoconfig.Overlay
type ConfigOverlayError = repoconfig.OverlayError
type ConfigOverride = repoconfig.Override
type ConfigReport = repoconfig.Report
type ConfigSelection = repoconfig.Selection
type ConfigWarning = repoconfig.Warning
type DefaultPermissionsAssumption = repoconfig.DefaultPermissionsAssumption
type FilesConfig = repoconfig.FilesConfig
type IgnoreConfig = repoconfig.IgnoreConfig
type IgnoreConfigs = repoconfig.IgnoreConfigs
type IgnorePatterns = repoconfig.IgnorePatterns
type JobTimeoutPolicy = repoconfig.JobTimeoutPolicy
type LintConfig = repoconfig.LintConfig
type LintRulesConfig = repoconfig.LintRulesConfig
type LocalActionSources = repoconfig.LocalActionSources
type PathConfig = repoconfig.PathConfig
type PermissionsPolicy = repoconfig.PermissionsPolicy
type Policy = repoconfig.Policy
type RequiredActionsOptions = repoconfig.RequiredActionsOptions
type RuffToolConfig = repoconfig.RuffToolConfig
type RuleGroupConfig = repoconfig.Group
type RuleLevel = repoconfig.Level
type RulePreset = repoconfig.Preset
type RulePresets = repoconfig.Presets
type RuleSetting = repoconfig.Setting
type SelfHostedRunnerConfig = repoconfig.SelfHostedRunnerConfig
type ShellcheckConfig = repoconfig.ShellcheckConfig
type ShellcheckConfigSource = repoconfig.ShellcheckConfigSource
type ShellcheckToolConfig = repoconfig.ShellcheckToolConfig
type SuppressionsPolicy = repoconfig.SuppressionsPolicy
type ToolsConfig = repoconfig.ToolsConfig

const (
	DefaultPermissionsAssumptionUnset      = repoconfig.DefaultPermissionsAssumptionUnset
	DefaultPermissionsAssumptionRestricted = repoconfig.DefaultPermissionsAssumptionRestricted
	DefaultPermissionsAssumptionPermissive = repoconfig.DefaultPermissionsAssumptionPermissive
)

// ParseConfig decodes a standalone configuration document.
func ParseConfig(b []byte) (*Config, error) { return repoconfig.Parse(b) }

// ReadConfigFile reads a configuration file and resolves its inheritance.
func ReadConfigFile(path string) (*Config, error) { return repoconfig.Load(path) }

// ParseConfigOverlay parses one configuration input supplied outside a config file.
func ParseConfigOverlay(input string, content []byte) (ConfigOverlay, error) {
	return repoconfig.ParseOverlay(input, content)
}

// ConfigKeys returns the supported top-level configuration keys in declaration order.
func ConfigKeys() []string { return repoconfig.Keys() }

// SelectedConfigPath finds the selected config without parsing its contents.
func SelectedConfigPath(selection ConfigSelection) (string, error) {
	return repoconfig.SelectedPath(selection)
}

// InspectConfig validates settings and optionally records their YAML origins.
func InspectConfig(selection ConfigSelection, withOrigin bool) (ConfigInspection, error) {
	return repoconfig.Inspect(selection, withOrigin)
}

// RequireJobTimeout enables the job timeout policy with an optional upper bound.
func RequireJobTimeout(maxMinutes float64) *JobTimeoutPolicy {
	return repoconfig.RequireJobTimeout(maxMinutes)
}

// RequireJobTimeoutRange requires job timeouts within the inclusive bounds.
func RequireJobTimeoutRange(minMinutes, maxMinutes float64) (*JobTimeoutPolicy, error) {
	return repoconfig.RequireJobTimeoutRange(minMinutes, maxMinutes)
}

// RequirePermissions enables the policy with "workflow" or "job" scope.
func RequirePermissions(scope string) (*PermissionsPolicy, error) {
	return repoconfig.RequirePermissions(scope)
}

// DisallowSuppressions enables suppression restrictions for the selected rules.
func DisallowSuppressions(report string, rules ...string) (*SuppressionsPolicy, error) {
	return repoconfig.DisallowSuppressions(report, rules...)
}
