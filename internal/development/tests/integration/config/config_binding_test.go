package config_test

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func decodeConfigYAML(data []byte, target any) error {
	parsed, err := document.Parse("config.yaml", data)
	if err != nil {
		return err
	}
	return binding.Decode(parsed.Value, target)
}

func encodeConfigYAML(value any) ([]byte, error) {
	raw, err := binding.Encode(value)
	if err != nil {
		return nil, err
	}
	return document.Marshal("config.yaml", raw)
}
