package tree

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"

	"github.com/kjanat/go-yaml/v4"
	"github.com/tidwall/jsonc"
)

// Decode parses one configuration document into a node tree.
// JSON and JSONC share comment and trailing-comma support.
// An empty document is an empty mapping.
func Decode(path string, data []byte) (*yaml.Node, error) {
	switch strings.ToLower(address.Extension(path)) {
	case ".toml":
		return decodeTOML(data)
	case ".json", ".jsonc":
		data = jsonc.ToJSON(data)
		if len(bytes.TrimSpace(data)) != 0 {
			var document json.RawMessage
			if err := json.Unmarshal(data, &document); err != nil {
				return nil, err
			}
		}
	}
	return decodeYAML(data)
}

func decodeYAML(data []byte) (*yaml.Node, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("must contain one document")
	}
	if len(document.Content) > 0 {
		return document.Content[0], nil
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
}
