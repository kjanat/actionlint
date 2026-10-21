// Package config loads and describes actionlint configuration.
package config

import (
	"actionlint.kjanat.dev/internal/lintconfig"
	"actionlint.kjanat.dev/internal/repoconfig"
	"actionlint.kjanat.dev/internal/toolconfig"
)

type Config = repoconfig.Config
type Inspection = repoconfig.Inspection
type Origin = repoconfig.Origin
type Overlay = repoconfig.Overlay

// OverlayError identifies individually valid inputs whose merged configuration is invalid.
type OverlayError = repoconfig.OverlayError
type ConfigOverride = repoconfig.Override
type Report = repoconfig.Report
type Selection = repoconfig.Selection
type Warning = repoconfig.Warning
type DefaultPermissionsAssumption = repoconfig.DefaultPermissionsAssumption
type FilesConfig = repoconfig.FilesConfig
type IgnoreConfig = repoconfig.IgnoreConfig
type IgnoreConfigs = repoconfig.IgnoreConfigs
type JobTimeoutPolicy = repoconfig.JobTimeoutPolicy
type LintConfig = repoconfig.LintConfig
type LintRulesConfig = repoconfig.LintRulesConfig
type LocalActionSources = repoconfig.LocalActionSources
type PathConfig = repoconfig.PathConfig
type PermissionsPolicy = repoconfig.PermissionsPolicy
type Policy = repoconfig.Policy
type RuffToolConfig = repoconfig.RuffToolConfig
type RuleGroupConfig = repoconfig.Group
type RuleLevel = repoconfig.Level
type RulePreset = repoconfig.Preset
type Presets = repoconfig.Presets
type RuleSetting = repoconfig.Setting

// RequiredActionsOptions selects action names or action/ref patterns required by a rule.
type RequiredActionsOptions = repoconfig.RequiredActionsOptions

// CommitHashOptions selects exceptions to immutable action references.
type CommitHashOptions = lintconfig.CommitHashOptions
type SelfHostedRunnerConfig = repoconfig.SelfHostedRunnerConfig
type ShellcheckConfig = repoconfig.ShellcheckConfig
type ShellcheckConfigSource = repoconfig.ShellcheckConfigSource
type ShellcheckToolConfig = repoconfig.ShellcheckToolConfig
type SuppressionsPolicy = repoconfig.SuppressionsPolicy
type ToolsConfig = repoconfig.ToolsConfig
type ToolModule = toolconfig.Module

// ToolBackend selects the configured transport for an external tool.
type ToolBackend = toolconfig.Backend

// ToolCommand describes a literal executable, arguments and environment.
type ToolCommand = toolconfig.Command

const (
	DefaultPermissionsAssumptionUnset      = repoconfig.DefaultPermissionsAssumptionUnset
	DefaultPermissionsAssumptionRestricted = repoconfig.DefaultPermissionsAssumptionRestricted
	DefaultPermissionsAssumptionPermissive = repoconfig.DefaultPermissionsAssumptionPermissive
)

// Parse decodes a standalone configuration document. Use Load to resolve extends.
// registeredRules adds caller-registered plugin IDs to the built-in rule catalog.
func Parse(source []byte, registeredRules ...string) (*Config, error) {
	return repoconfig.Parse(source, registeredRules...)
}

// ParseOverlay validates a complete config document or one named configuration input.
// registeredRules adds caller-registered plugin IDs to the built-in rule catalog.
func ParseOverlay(input string, source []byte, registeredRules ...string) (Overlay, error) {
	return repoconfig.ParseOverlay(input, source, registeredRules...)
}

// Load reads a configuration file and resolves its inheritance.
// registeredRules adds caller-registered plugin IDs to the built-in rule catalog.
func Load(path string, registeredRules ...string) (*Config, error) {
	return repoconfig.Load(path, registeredRules...)
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

// ShellcheckConfigPath selects a nonempty, single-line rc file or directory path.
func ShellcheckConfigPath(path string) (*ShellcheckConfigSource, error) {
	return repoconfig.ShellcheckConfigPath(path)
}

// ShellcheckConfigInline validates inline settings and owns a copy of their values.
func ShellcheckConfigInline(settings ShellcheckConfig) (*ShellcheckConfigSource, error) {
	return repoconfig.ShellcheckConfigInline(settings)
}
