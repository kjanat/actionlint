package python

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
)

// VarTargetVersion holds the Python target version the step shell selects,
// such as py312.
const VarTargetVersion = "target_version"

const engineTargetKey = "target-version"

func Run(text string, shell *string) (preparation.Prepared, error) {
	if shell == nil || strings.Contains(*shell, "${{") || !IsShell(*shell) {
		return preparation.Prepared{Skip: "its step shell does not run Python"}, nil
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
		settings = map[string]any{}
	}
	if target != "" && settings[engineTargetKey] == nil {
		settings[engineTargetKey] = target
	}
	return json.Marshal(settings)
}
