package manifests

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/configschema"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

// Settings returns the user configuration described by this manifest.
func (m Manifest) Settings() toolsettings.Settings {
	return (toolsettings.Settings{Schema: m.ConfigSchema, Shorthands: m.ConfigShorthands, ReadsFiles: m.ReadsConfigFiles()}).Clone()
}

// ConfigurationSchemaProvider returns the part of this manifest that the
// printed configuration schema describes under tools.<name> and lint.rules.
func (m Manifest) ConfigurationSchemaProvider() configschema.ToolProvider {
	provider := configschema.ToolProvider{Rule: m.Rule, Settings: m.Settings()}
	if m.Process != nil {
		provider.ProcessSchema = m.Process.ConfigSchema
	}
	return provider
}
