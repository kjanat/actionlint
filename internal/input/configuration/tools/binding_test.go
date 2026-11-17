package tools

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func decodeBackendConfig(source []byte, dst any) error {
	parsed, err := document.ParseConfigDocument("test.yaml", source)
	if err != nil {
		return err
	}
	return configdecode.Decode(parsed.Value(), dst)
}

func encodeBackendConfig(value any) ([]byte, error) {
	values, err := configdecode.Encode(value)
	if err != nil {
		return nil, err
	}
	return document.Marshal("test.yaml", values)
}
