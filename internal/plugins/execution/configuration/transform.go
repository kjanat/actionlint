// Package configuration validates integration settings and runs their declared
// transformation before a process or component receives them.
package configuration

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/jq"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

// ReadAccess identifies configured directories. The host separately checks that
// each requested directory is inside the workspace before opening it.
type ReadAccess struct {
	Enabled     *bool    `json:"enabled,omitempty"`
	Directories []string `json:"directories,omitempty"`
}

// Settings contains the tool options, generated comments and read-access
// requests returned by an integration.
type Settings struct {
	Options    json.RawMessage `json:"options"`
	Prefix     string          `json:"source_prefix,omitempty"`
	Language   *string         `json:"language,omitempty"`
	ReadAccess ReadAccess      `json:"read_access"`
}

type Transform struct {
	schema  *configschema.Schema
	paths   []string
	program *jq.Program
}

func New(m manifests.Manifest, programs manifests.Programs) (*Transform, error) {
	t := &Transform{paths: append([]string(nil), m.ConfigurationPaths...)}
	for _, path := range t.paths {
		if path == "" || !jsontext.Pointer(path).IsValid() {
			return nil, fmt.Errorf("configuration path %q must be a JSON pointer to a property", path)
		}
	}
	if len(m.ConfigSchema) != 0 {
		var err error
		t.schema, err = configschema.Compile(m.ConfigSchema)
		if err != nil {
			return nil, err
		}
	}
	if m.ConfigurationTransform != nil {
		var err error
		source, loadErr := manifests.ResolveTransform(m.ConfigurationTransform, programs)
		if loadErr != nil {
			return nil, loadErr
		}
		t.program, err = jq.New(source, 1<<20)
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}

// Apply validates settings and expands the path options declared in the
// manifest. The transform receives config and config_file.
func (t *Transform) Apply(ctx context.Context, inline json.RawMessage, file string, expand func(string) (string, error)) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return Settings{}, err
	}
	if len(inline) == 0 {
		inline = json.RawMessage(`{}`)
	}
	if t.schema != nil {
		if err := t.schema.Validate(inline); err != nil {
			return Settings{}, err
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(inline))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return Settings{}, err
	}
	if expand != nil {
		for _, pointer := range t.paths {
			if err := expandOption(value, pointer, expand); err != nil {
				return Settings{}, err
			}
		}
	}
	if t.program == nil {
		data, err := json.Marshal(value)
		return Settings{Options: data}, err
	}
	data, err := t.program.Apply(ctx, map[string]any{"config": value, "config_file": file}, map[string]any{})
	if err != nil {
		return Settings{}, err
	}
	var settings Settings
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("configuration transform: %w", err)
	}
	var options map[string]any
	if err := json.Unmarshal(settings.Options, &options); err != nil || options == nil {
		return Settings{}, fmt.Errorf("configuration transform must return an options object")
	}
	if settings.Prefix != "" && !strings.HasSuffix(settings.Prefix, "\n") {
		return Settings{}, fmt.Errorf("configuration transform source_prefix must end with a newline")
	}
	return settings, nil
}

// expandOption follows a JSON pointer into validated options. An absent optional
// property needs no expansion; each selected value must be a string or array.
func expandOption(value any, pointer string, expand func(string) (string, error)) error {
	parts := slices.Collect(jsontext.Pointer(pointer).Tokens())
	for position, token := range parts {
		var field any
		var assign func(any)
		switch container := value.(type) {
		case map[string]any:
			var present bool
			field, present = container[token]
			if !present || field == nil {
				return nil
			}
			assign = func(replacement any) { container[token] = replacement }
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || strconv.Itoa(index) != token {
				return fmt.Errorf("configuration path %q has an invalid array index", pointer)
			}
			if index >= len(container) {
				return nil
			}
			field = container[index]
			if field == nil {
				return nil
			}
			assign = func(replacement any) { container[index] = replacement }
		default:
			return fmt.Errorf("configuration path %q crosses a non-container value", pointer)
		}
		if position < len(parts)-1 {
			value = field
			continue
		}
		switch field := field.(type) {
		case string:
			replacement, err := expand(field)
			if err != nil {
				return fmt.Errorf("%s: %w", pointer, err)
			}
			assign(replacement)
		case []any:
			for position, item := range field {
				text, ok := item.(string)
				if !ok {
					return fmt.Errorf("%s/%d: expected a path string", pointer, position)
				}
				replacement, err := expand(text)
				if err != nil {
					return fmt.Errorf("%s/%d: %w", pointer, position, err)
				}
				field[position] = replacement
			}
		default:
			return fmt.Errorf("%s: expected a path string or array of strings", pointer)
		}
	}
	return nil
}
