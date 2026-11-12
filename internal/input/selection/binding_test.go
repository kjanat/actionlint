package selection

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func decodeSelectionConfig(source []byte, dst any) error {
	parsed, err := document.Parse("test.yaml", source)
	if err != nil {
		return err
	}
	return binding.Decode(parsed.Value, dst)
}

func encodeSelectionConfig(value any) ([]byte, error) {
	values, err := binding.Encode(value)
	if err != nil {
		return nil, err
	}
	return document.Marshal("test.yaml", values)
}
