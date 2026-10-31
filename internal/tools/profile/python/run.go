package python

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/profile"
)

// VarTargetVersion is the Python target version the step shell selects,
// such as py312; unset when the shell leaves it open.
const VarTargetVersion = "target_version"

// engineTargetKey is the configuration key that carries the target version
// to a snippet engine, which receives no argument variables.
const engineTargetKey = "target-version"

// Run prepares a run block whose step shell runs Python. Other run blocks,
// and those whose shell is a template, are skipped.
func Run(text string, shell *string) (profile.Prepared, error) {
	if shell == nil || strings.Contains(*shell, "${{") || !IsShell(*shell) {
		return profile.Prepared{Skip: "its step shell does not run Python"}, nil
	}
	prepared, err := Script(text)
	if err != nil || prepared.Skip != "" {
		return prepared, err
	}
	if target := ShellTarget(*shell); target != "" {
		prepared.Vars = map[string]string{VarTargetVersion: target}
	}
	return prepared, nil
}

// EngineConfig is a snippet engine's configuration object: config with the
// target version a run block's shell selects, unless config sets one.
func EngineConfig(config json.RawMessage, vars map[string]string) (json.RawMessage, error) {
	target := vars[VarTargetVersion]
	settings := map[string]any{}
	if len(bytes.TrimSpace(config)) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(config))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return nil, fmt.Errorf("engine configuration: %w", err)
		}
	}
	if settings == nil {
		// A null configuration selects no settings.
		settings = map[string]any{}
	}
	if target != "" && settings[engineTargetKey] == nil {
		settings[engineTargetKey] = target
	}
	return json.Marshal(settings)
}
