package tree

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/config/address"

	"github.com/kjanat/go-yaml/v4"
)

// Decode parses one configuration document into a node tree. The file
// extension selects the format: .toml is TOML, everything else is YAML, which
// includes JSON. An empty document is an empty mapping.
func Decode(path string, data []byte) (*yaml.Node, error) {
	if strings.EqualFold(address.Extension(path), ".toml") {
		return decodeTOML(data)
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
