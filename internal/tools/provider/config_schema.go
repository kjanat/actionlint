package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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

// validateConfigShorthands requires every shorthand to name a property the
// configuration schema declares and none to shadow a host-owned key.
func validateConfigShorthands(m Manifest) error {
	if len(m.ConfigShorthands) == 0 {
		return nil
	}
	if len(m.ConfigSchema) == 0 {
		return errors.New("config_shorthands requires config_schema")
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(m.ConfigSchema, &schema); err != nil {
		return fmt.Errorf("config_schema: %w", err)
	}
	for _, key := range m.ConfigShorthands {
		if slices.Contains(hostConfigKeys, key) {
			return fmt.Errorf("config_shorthands: %q is a host-owned key", key)
		}
		if _, ok := schema.Properties[key]; !ok {
			return fmt.Errorf("config_shorthands: config_schema declares no property %q", key)
		}
	}
	return nil
}
