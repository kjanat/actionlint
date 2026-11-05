// Package plugins reads and edits plugin declarations in configuration files.
package plugins

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tree"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
	"github.com/kjanat/go-yaml/v4"
)

// References reads the effective plugin list and reports whether it was declared.
func References(node *yaml.Node) ([]pluginref.Reference, bool, error) {
	if err := ValidateRoot(node); err != nil {
		return nil, false, err
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "plugins" {
			var declarations config.PluginDeclarations
			if err := declarations.UnmarshalYAML(node.Content[i+1]); err != nil {
				return nil, true, err
			}
			return []pluginref.Reference(declarations), true, nil
		}
	}
	return nil, false, nil
}

// ValidateRoot rejects non-mapping roots, non-string keys and duplicate keys.
func ValidateRoot(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("config must be a mapping")
	}
	keys := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.ShortTag() != "!!str" && key.ShortTag() != "!!merge" {
			return errors.New("config keys must be strings")
		}
		if keys[key.Value] {
			return fmt.Errorf("duplicate config key %q", key.Value)
		}
		keys[key.Value] = true
	}
	return nil
}

func pluginManifestSequence(refs []pluginref.Reference) (*yaml.Node, error) {
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, ref := range refs {
		normalized, err := ref.Normalize()
		if err != nil {
			return nil, err
		}
		if normalized.DirectSource() != "" || normalized.Registry() == pluginref.DefaultRegistry {
			sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: normalized.String()})
			continue
		}
		entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		pairs := [][2]string{{"name", normalized.Name()}, {"version", normalized.Version()}, {"registry", normalized.Registry()}}
		if digest := normalized.CandidateDigest(); digest != "" {
			pairs = append(pairs, [2]string{"sha256", digest})
		}
		for _, pair := range pairs {
			entry.Content = append(entry.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pair[0]},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pair[1]})
		}
		sequence.Content = append(sequence.Content, entry)
	}
	return sequence, nil
}

// EditManifest replaces plugin declarations without changing other settings.
func EditManifest(path string, original []byte, refs []pluginref.Reference) ([]byte, error) {
	node, err := tree.Decode(path, original)
	if err != nil {
		return nil, err
	}
	if err := ValidateRoot(node); err != nil {
		return nil, err
	}
	// YAML serialization consumes scalar tag hints. Capture the original
	// settings before an editor can mutate the nodes it serializes.
	before, err := tree.Expand(node, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	sequence, err := pluginManifestSequence(refs)
	if err != nil {
		return nil, err
	}
	var edited []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		edited, err = editJSONPlugins(original, sequence)
	case ".toml":
		edited, err = editTOMLPlugins(original, sequence)
	default:
		edited, err = editYAMLPlugins(original, node, sequence)
	}
	if err != nil {
		return nil, err
	}
	updated, err := tree.Decode(path, edited)
	if err != nil {
		return nil, fmt.Errorf("cannot safely edit plugin config: %w", err)
	}
	after, err := tree.Expand(updated, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	if !sameManifestSettings(withoutManifestPlugins(before), withoutManifestPlugins(after)) {
		return nil, errors.New("cannot edit plugins without changing other config settings")
	}
	actual, declared, err := References(after)
	if err != nil {
		return nil, fmt.Errorf("invalid edited plugin declarations: %w", err)
	}
	if !declared {
		return nil, errors.New("edited config contains no plugin declarations")
	}
	expected, _, err := References(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "plugins"}, sequence,
	}})
	if err != nil {
		return nil, err
	}
	if !slices.Equal(actual, expected) {
		return nil, errors.New("edited plugin declarations differ from requested plugins")
	}
	return edited, nil
}

func withoutManifestPlugins(node *yaml.Node) *yaml.Node {
	copyNode := *node
	copyNode.Content = nil
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value != "plugins" {
			copyNode.Content = append(copyNode.Content, node.Content[i], node.Content[i+1])
		}
	}
	return &copyNode
}

func sameManifestSettings(left, right *yaml.Node) bool {
	if left.Kind != right.Kind || left.ShortTag() != right.ShortTag() || left.Value != right.Value || len(left.Content) != len(right.Content) {
		return false
	}
	for i := range left.Content {
		if !sameManifestSettings(left.Content[i], right.Content[i]) {
			return false
		}
	}
	return true
}

func spliceManifest(original []byte, start, end int, replacement []byte) []byte {
	var result []byte
	result = append(result, original[:start]...)
	result = append(result, replacement...)
	return append(result, original[end:]...)
}
