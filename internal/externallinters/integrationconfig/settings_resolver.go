// Package integrationconfig validates integration settings and runs their declared transformation.
package integrationconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/externallinters/jq"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

// ReadAccess directories still need the host's workspace check before opening.
type ReadAccess struct {
	Enabled     *bool    `json:"enabled,omitempty"`
	Directories []string `json:"directories,omitempty"`
}

type ResolvedSettings struct {
	Options    json.RawMessage `json:"options"`
	Prefix     string          `json:"source_prefix,omitempty"`
	Language   *string         `json:"language,omitempty"`
	ReadAccess ReadAccess      `json:"read_access"`
}

type SettingsResolver struct {
	schema  *toolsettings.Schema
	paths   []string
	program *jq.Program
}

func New(m toolmanifest.Manifest, programs toolmanifest.Programs) (*SettingsResolver, error) {
	t := &SettingsResolver{paths: append([]string(nil), m.ConfigurationPaths...)}
	for _, path := range t.paths {
		if path == "" || !jsontext.Pointer(path).IsValid() {
			return nil, fmt.Errorf("configuration path %q must be a JSON pointer to a property", path)
		}
	}
	if len(m.ConfigSchema) != 0 {
		var err error
		t.schema, err = toolsettings.Compile(m.ConfigSchema)
		if err != nil {
			return nil, err
		}
	}
	if m.ConfigurationTransform != nil {
		var err error
		source, loadErr := toolmanifest.ResolveTransform(m.ConfigurationTransform, programs)
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

// Resolve passes config and config_file to the configuration_transform program.
func (t *SettingsResolver) Resolve(ctx context.Context, inline json.RawMessage, file string, expand func(string) (string, error)) (ResolvedSettings, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSettings{}, err
	}
	if len(inline) == 0 {
		inline = json.RawMessage(`{}`)
	}
	if t.schema != nil {
		if err := t.schema.Validate(inline); err != nil {
			return ResolvedSettings{}, err
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(inline))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ResolvedSettings{}, err
	}
	if expand != nil {
		for _, pointer := range t.paths {
			if err := expandOption(value, pointer, expand); err != nil {
				return ResolvedSettings{}, err
			}
		}
	}
	if t.program == nil {
		data, err := json.Marshal(value)
		return ResolvedSettings{Options: data}, err
	}
	data, err := t.program.Apply(ctx, map[string]any{"config": value, "config_file": file}, map[string]any{})
	if err != nil {
		return ResolvedSettings{}, err
	}
	var settings ResolvedSettings
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return ResolvedSettings{}, fmt.Errorf("configuration transform: %w", err)
	}
	var options map[string]any
	if err := json.Unmarshal(settings.Options, &options); err != nil || options == nil {
		return ResolvedSettings{}, errors.New("configuration transform must return an options object")
	}
	if settings.Prefix != "" && !strings.HasSuffix(settings.Prefix, "\n") {
		return ResolvedSettings{}, errors.New("configuration transform source_prefix must end with a newline")
	}
	return settings, nil
}

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
