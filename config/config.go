// Package config loads validated configuration. Repository facts belong to
// package repository; analysis backends belong to package analysis.
package config

import (
	configreport "actionlint.kjanat.dev/v2/config/report"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	configvalue "actionlint.kjanat.dev/v2/internal/config/value"
)

// Config is an opaque validated document. Its zero value selects defaults.
type Config = configvalue.Document

// Overlay is an immutable partial configuration document.
type Overlay = repoconfig.Overlay

// OverlayError identifies valid inputs whose combined configuration is invalid.
type OverlayError = repoconfig.OverlayError

// Selection chooses a configuration file or disables discovery.
type Selection = repoconfig.Selection

// Presets selects analysis defaults without modifying configuration.
type Presets = repoconfig.Presets

// Parse validates YAML or JSON. Use Load for documents containing extends.
// additionalRules supplies registered analyzer names for this document only.
func Parse(source []byte, additionalRules ...string) (*Config, error) {
	value, err := repoconfig.Parse(source, additionalRules...)
	if err != nil {
		return nil, err
	}
	return configvalue.Wrap(value), nil
}

// Load resolves local inheritance and validates the resulting configuration.
func Load(path string, additionalRules ...string) (*Config, error) {
	value, err := repoconfig.Load(path, additionalRules...)
	if err != nil {
		return nil, err
	}
	return configvalue.Wrap(value), nil
}

// ParseOverlay validates a complete document or one named configuration section.
func ParseOverlay(input string, source []byte, additionalRules ...string) (Overlay, error) {
	return repoconfig.ParseOverlay(input, source, additionalRules...)
}

// Inspect reports effective settings and optional source locations.
func Inspect(selection Selection, origins bool, registeredRules ...string) (configreport.Inspection, error) {
	return repoconfig.Inspect(selection, origins, registeredRules...)
}
