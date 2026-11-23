package toolmanifest

import (
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

func (m Manifest) Settings() toolsettings.Spec {
	return (toolsettings.Spec{Schema: m.ConfigSchema, Shorthands: m.ConfigShorthands, ReadsFiles: m.ReadsConfigFiles()}).Clone()
}

func (m Manifest) ConfigurationSchemaProvider() toolsettings.Provider {
	provider := toolsettings.Provider{Rule: m.Rule, Settings: m.Settings()}
	if m.Process != nil {
		provider.ProcessSchema = m.Process.ConfigSchema
	}
	return provider
}
