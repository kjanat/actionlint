package manifests

import "actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"

// Settings returns the user configuration described by this manifest.
func (m Manifest) Settings() configschema.Settings {
	return (configschema.Settings{Schema: m.ConfigSchema, Shorthands: m.ConfigShorthands, ReadsFiles: m.ReadsConfigFiles()}).Clone()
}
