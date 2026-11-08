package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kjanat/go-yaml/v4"
	"github.com/tidwall/jsonc"
)

func manifestJSONValue(sequence *yaml.Node) ([]byte, error) {
	entries := make([]json.RawMessage, 0, len(sequence.Content))
	for _, entry := range sequence.Content {
		var data []byte
		var err error
		if entry.Kind == yaml.ScalarNode {
			data, err = json.Marshal(entry.Value)
		} else {
			mapping := map[string]string{}
			for i := 0; i < len(entry.Content); i += 2 {
				mapping[entry.Content[i].Value] = entry.Content[i+1].Value
			}
			data, err = json.Marshal(mapping)
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, data)
	}
	return json.Marshal(entries)
}

func editJSONPlugins(original []byte, sequence *yaml.Node) ([]byte, error) {
	value, err := manifestJSONValue(sequence)
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
