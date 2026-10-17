package ruff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// UnsupportedConfigOptions matches the read-only boundary of actionlint-plugin-ruff.
// Ruff itself validates option names, values, and rule selectors.
func UnsupportedConfigOptions() []string {
	return []string{"extend", "cache-dir", "src", "namespace-packages", "fix", "fix-only", "unsafe-fixes", "show-fixes", "output-file"}
}

func (c Config) NativeArguments() ([]string, error) {
	for _, key := range UnsupportedConfigOptions() {
		if _, exists := c.Native[key]; exists {
			return nil, fmt.Errorf("tools.ruff.config: %q is unavailable in isolated, read-only analysis", key)
		}
	}
	// Normalize API-provided typed maps and slices before TOML encoding.
	// UseNumber avoids rounding integer option values through float64.
	native, err := cloneOptions(c.Native)
	if err != nil {
		return nil, err
	}
	var args []string
	for _, key := range slices.Sorted(maps.Keys(native)) {
		if native[key] == nil {
			continue
		}
		value, err := inlineTOML(native[key])
		if err != nil {
			return nil, fmt.Errorf("tools.ruff.config.%s: %w", key, err)
		}
		encoded, _ := json.Marshal(key)
		args = append(args, "--config", string(encoded)+" = "+value)
	}
	return args, nil
}

// inlineTOML encodes one literal argument. It never constructs shell source or
// a filename; Ruff receives its own options through inline --config overrides.
func inlineTOML(value any) (string, error) {
	switch value := value.(type) {
	case map[string]any:
		entries := make([]string, 0, len(value))
		for _, key := range slices.Sorted(maps.Keys(value)) {
			if value[key] == nil {
				continue
			}
			item, err := inlineTOML(value[key])
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(key)
			entries = append(entries, string(encoded)+" = "+item)
		}
		return "{" + strings.Join(entries, ", ") + "}", nil
	case []any:
		entries := make([]string, len(value))
		for i, entry := range value {
			item, err := inlineTOML(entry)
			if err != nil {
				return "", err
			}
			entries[i] = item
		}
		return "[" + strings.Join(entries, ", ") + "]", nil
	case nil:
		return "", errors.New("null array entries are not Ruff options")
	default:
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
}

// EngineOptions projects legacy settings into Ruff's own configuration. The
// JSON round trip also isolates nested maps from a caller-owned config.
func (c Config) EngineOptions() (map[string]any, error) {
	options, err := cloneOptions(c.Native)
	if err != nil {
		return nil, err
	}
	if options["target-version"] == nil {
		target := c.TargetVersion
		if target == "" {
			target = "py314"
		}
		options["target-version"] = target
	}
	if c.Select != nil || c.Ignore != nil {
		lint, ok := options["lint"].(map[string]any)
		if !ok && options["lint"] != nil {
			return nil, errors.New("tools.ruff.config.lint must be a mapping")
		}
		if lint == nil {
			lint = map[string]any{}
		}
		if c.Select != nil {
			lint["select"] = slices.Clone(c.Select)
		}
		if c.Ignore != nil {
			lint["ignore"] = slices.Clone(c.Ignore)
		}
		options["lint"] = lint
	}
	return options, nil
}

func cloneOptions(input map[string]any) (map[string]any, error) {
	options := map[string]any{}
	if input == nil {
		return options, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&options); err != nil {
		return nil, err
	}
	return options, nil
}
