package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/tidwall/jsonc"
)

func editJSONPlugins(original []byte, sequence []any) ([]byte, error) {
	value, err := json.Marshal(sequence)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(original)) == 0 {
		return append(append([]byte("{\n  \"plugins\": "), value...), []byte("\n}\n")...), nil
	}
	normalized := jsonc.ToJSON(original)
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("JSON config must be an object")
	}
	count := 0
	end := int(decoder.InputOffset())
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		start := int(decoder.InputOffset())
		for start < len(normalized) && (normalized[start] == ':' || strings.ContainsRune(" \r\n\t", rune(normalized[start]))) {
			start++
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		end = int(decoder.InputOffset())
		if key == "plugins" {
			return spliceManifest(original, start, int(decoder.InputOffset()), value), nil
		}
		count++
	}
	prefix := []byte("\n  \"plugins\": ")
	if count > 0 {
		prefix = append([]byte{','}, prefix...)
	}
	replacement := append(append(prefix, value...), '\n')
	return spliceManifest(original, end, end, replacement), nil
}
