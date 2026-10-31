package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// engineConfig is engine configuration after the manifest's transform. Value
// holds decoded JSON with exact numbers, or nil when nothing is configured.
// Vars override preparation variables; a nil value drops the variable.
type engineConfig struct {
	value any
	vars  map[string]*string
}

// decodeConfig decodes configuration JSON keeping numbers exact. Empty input
// is no configuration.
func decodeConfig(data []byte) (any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errors.New("configuration holds more than one JSON value")
	}
	return value, nil
}

// configTransform runs a manifest's configuration transform. Its program maps
// the configuration to {"config": ..., "vars": {...}} and sees the argument
// variables as $vars.
type configTransform struct{ program *jqProgram }

func (t *configTransform) apply(ctx context.Context, config any, vars map[string]string) (engineConfig, error) {
	output, err := t.program.run(ctx, jqValue(config), stringVariables(vars))
	if err != nil {
		return engineConfig{}, fmt.Errorf("configuration transform: %w", err)
	}
	decoded, err := decodeConfig(output)
	if err != nil {
		return engineConfig{}, fmt.Errorf("configuration transform output: %w", err)
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return engineConfig{}, errors.New(`configuration transform must produce an object {"config": ..., "vars": {...}}`)
	}
	for key := range object {
		if key != "config" && key != "vars" {
			return engineConfig{}, fmt.Errorf("configuration transform produced unknown member %q", key)
		}
	}
	value, present := object["config"]
	if !present {
		return engineConfig{}, errors.New(`configuration transform output is missing "config"`)
	}
	result := engineConfig{value: value}
	switch vars := object["vars"].(type) {
	case nil:
	case map[string]any:
		result.vars = make(map[string]*string, len(vars))
		for name, value := range vars {
			switch value := value.(type) {
			case nil:
				result.vars[name] = nil
			case string:
				result.vars[name] = &value
			default:
				return engineConfig{}, fmt.Errorf("configuration transform variable %q must be a string or null", name)
			}
		}
	default:
		return engineConfig{}, errors.New("configuration transform vars must be an object")
	}
	return result, nil
}

// jqValue converts decoded JSON numbers to the integer, big integer or float
// values gojq accepts.
func jqValue(value any) any {
	switch value := value.(type) {
	case json.Number:
		if integer, err := strconv.Atoi(value.String()); err == nil {
			return integer
		}
		if integer, ok := new(big.Int).SetString(value.String(), 10); ok {
			return integer
		}
		float, err := value.Float64()
		if err != nil {
			return value.String()
		}
		return float
	case map[string]any:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			converted[key] = jqValue(item)
		}
		return converted
	case []any:
		converted := make([]any, len(value))
		for i, item := range value {
			converted[i] = jqValue(item)
		}
		return converted
	default:
		return value
	}
}

// checkForbidden rejects configuration setting any dotted key path in
// forbidden, even to null.
func checkForbidden(name string, config any, forbidden []string) error {
	for _, path := range forbidden {
		value := config
		present := true
		for key := range strings.SplitSeq(path, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				present = false
				break
			}
			value, present = object[key]
			if !present {
				break
			}
		}
		if present {
			return fmt.Errorf("tools.%s.config: %q is unavailable to this tool's read-only analysis", name, path)
		}
	}
	return nil
}

// TOMLPairs renders configuration JSON as repeated option arguments, each one
// top-level key set to an inline TOML value: option, `"key" = value`. Keys
// are sorted and null values skipped. Every value is one literal argument, so
// configuration never becomes a command-line option of its own.
func TOMLPairs(option string, config json.RawMessage) ([]string, error) {
	value, err := decodeConfig(config)
	if err != nil {
		return nil, err
	}
	return tomlPairs(option, value)
}

func tomlPairs(option string, config any) ([]string, error) {
	if config == nil {
		return nil, nil
	}
	object, ok := config.(map[string]any)
	if !ok {
		return nil, errors.New("config must be an object to render as TOML")
	}
	var args []string
	for _, key := range slices.Sorted(maps.Keys(object)) {
		if object[key] == nil {
			continue
		}
		value, err := inlineTOML(object[key])
		if err != nil {
			return nil, fmt.Errorf("config.%s: %w", key, err)
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		args = append(args, option, string(encoded)+" = "+value)
	}
	return args, nil
}

// inlineTOML encodes one literal TOML value. It never constructs shell source
// or a filename.
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
			encoded, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
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
		return "", errors.New("null array entries have no TOML form")
	default:
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
}

// tomlValue converts decoded JSON to values go-toml encodes, dropping null
// object members, which TOML cannot express.
func tomlValue(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			return integer, nil
		}
		return value.Float64()
	case map[string]any:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			if item == nil {
				continue
			}
			item, err := tomlValue(item)
			if err != nil {
				return nil, err
			}
			converted[key] = item
		}
		return converted, nil
	case []any:
		converted := make([]any, len(value))
		for i, item := range value {
			if item == nil {
				return nil, errors.New("null array entries have no TOML form")
			}
			item, err := tomlValue(item)
			if err != nil {
				return nil, err
			}
			converted[i] = item
		}
		return converted, nil
	default:
		return value, nil
	}
}

// configDocument encodes configuration for a configuration file.
func configDocument(render provider.Render, config any) ([]byte, error) {
	switch render {
	case provider.RenderJSONFile:
		if config == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(config)
	case provider.RenderTOMLFile:
		if config == nil {
			return nil, nil
		}
		if _, ok := config.(map[string]any); !ok {
			return nil, errors.New("configuration rendered as TOML must be an object")
		}
		converted, err := tomlValue(config)
		if err != nil {
			return nil, err
		}
		return toml.Marshal(converted)
	case provider.RenderNone, provider.RenderTOMLPairs, provider.RenderJSONStdin:
		return nil, fmt.Errorf("configuration rendering %q writes no file", render)
	default:
		return nil, fmt.Errorf("unknown configuration rendering %q", render)
	}
}

// writeConfigFile writes a configuration document to a private temporary
// file and returns its path and a function removing it.
func writeConfigFile(render provider.Render, config any) (string, func(), error) {
	document, err := configDocument(render, config)
	if err != nil {
		return "", nil, err
	}
	pattern := "actionlint-config-*.json"
	if render == provider.RenderTOMLFile {
		pattern = "actionlint-config-*.toml"
	}
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	remove := func() { _ = os.Remove(file.Name()) }
	if _, err := file.Write(document); err != nil {
		_ = file.Close()
		remove()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, err
	}
	return file.Name(), remove, nil
}
