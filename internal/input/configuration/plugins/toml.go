package plugins

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

func pluginTableFields(entry any) ([][2]string, error) {
	var declarations configuration.PluginDeclarations
	if err := declarations.DecodeConfig([]any{entry}); err != nil {
		return nil, err
	}
	ref := declarations[0]
	if ref.DirectSource() != "" {
		return [][2]string{{"source", ref.DirectSource()}}, nil
	}
	fields := [][2]string{{"name", ref.Name()}, {"version", ref.Version()}}
	if ref.Registry() != pluginreference.DefaultRegistry {
		fields = append(fields, [2]string{"registry", ref.Registry()})
	}
	if digest := ref.CandidateDigest(); digest != "" {
		fields = append(fields, [2]string{"sha256", digest})
	}
	return fields, nil
}
