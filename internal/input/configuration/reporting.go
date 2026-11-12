package config

import (
	"fmt"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"
)

// OutputFormats lists the diagnostic representations selected by default.
type OutputFormats []string

// OutputConfig supplies defaults for command-line reporting.
type OutputConfig struct {
	// Formats selects diagnostic representations in order. Omission uses the CLI default.
	Formats OutputFormats `yaml:"formats,omitempty" jsonschema:"nullable"`
	// LogLevel selects info, debug, or none for CLI logging.
	LogLevel *string `yaml:"log-level,omitempty" jsonschema:"nullable,enum=info,enum=debug,enum=none"`
	// Summary controls the final summary.
	Summary *bool `yaml:"summary,omitempty" jsonschema:"nullable"`
	// NetworkStats controls the network statistics report.
	NetworkStats *bool `yaml:"network-stats,omitempty" jsonschema:"nullable"`
	// Quiet suppresses progress logs and summaries.
	Quiet *bool `yaml:"quiet,omitempty" jsonschema:"nullable"`
}

// DecodeConfig validates reporting defaults and preserves omitted values.
func (c *OutputConfig) DecodeConfig(value any) error {
	type plain OutputConfig
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	for i, name := range next.Formats {
		if !outputformat.Valid(name) {
			return binding.At("formats", binding.At(strconv.Itoa(i), fmt.Errorf("unknown output format %q", name)))
		}
	}
	if next.LogLevel != nil && *next.LogLevel != "info" && *next.LogLevel != "debug" && *next.LogLevel != "none" {
		return binding.At("log-level", fmt.Errorf("invalid log level %q: choose info, debug or none", *next.LogLevel))
	}
	*c = OutputConfig(next)
	return nil
}
