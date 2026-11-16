package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/tidwall/jsonc"
)

// setJSONList replaces or appends the top-level member key of a JSON object.
// Comments and trailing commas elsewhere are kept.
func setJSONList(original []byte, member string, sequence []any) ([]byte, error) {
	value, err := json.Marshal(sequence)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(original)) == 0 {
		name, err := json.Marshal(member)
		if err != nil {
			return nil, err
		}
		return append(append(append(append([]byte("{\n  "), name...), ": "...), value...), []byte("\n}\n")...), nil
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
		if key == member {
			return splice(original, start, int(decoder.InputOffset()), value), nil
		}
		count++
	}
	name, err := json.Marshal(member)
	if err != nil {
		return nil, err
	}
	prefix := append(append([]byte("\n  "), name...), ": "...)
	if count > 0 {
		prefix = append([]byte{','}, prefix...)
	}
	replacement := append(append(prefix, value...), '\n')
	return splice(original, end, end, replacement), nil
}
