package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
)

// hostConfigKeys are the tools.<name> keys the host owns. A shorthand cannot
// shadow them.
var hostConfigKeys = []string{"enabled", "backend", "config"}

// validateConfigSchema compiles an engine configuration schema without
// granting it filesystem or network reads.
func validateConfigSchema(field string, schema json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	if _, err := configschema.Compile(schema); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

// schemaProperties is the part of a configuration schema a shorthand path
// walks: the properties declared at each nesting level.
type schemaProperties struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

// validateConfigShorthands requires every shorthand to set a property the
// configuration schema declares, at every level of its path, and none to
// shadow a host-owned key.
func validateConfigShorthands(m Manifest) error {
	if len(m.ConfigShorthands) == 0 {
		return nil
	}
	if len(m.ConfigSchema) == 0 {
		return errors.New("config_shorthands requires config_schema")
	}
	for _, key := range slices.Sorted(maps.Keys(m.ConfigShorthands)) {
		if slices.Contains(hostConfigKeys, key) {
			return fmt.Errorf("config_shorthands: %q is a host-owned key", key)
		}
		schema := m.ConfigSchema
		for _, property := range m.ConfigShorthands[key].Path {
			var level schemaProperties
			if err := json.Unmarshal(schema, &level); err != nil {
				return fmt.Errorf("config_schema: %w", err)
			}
			next, ok := level.Properties[property]
			if !ok {
				return fmt.Errorf("config_shorthands.%s: config_schema declares no property %q", key, strings.Join(m.ConfigShorthands[key].Path, "."))
			}
			schema = next
		}
	}
	return nil
}
