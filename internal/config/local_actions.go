package config

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/internal/localaction"
	"go.yaml.in/yaml/v4"
)

// LocalActionSources maps generated local action directories to source directories.
type LocalActionSources map[string]string

// Canonical keys make equivalent ./ and $/ aliases override each other during extends merging.
// Each source document has already been validated before this function runs.
func normalizeLocalActionSourceNode(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		if normalized, err := localaction.Normalize(node.Content[i].Value); err == nil {
			node.Content[i].Value = normalized
		}
	}
}

func normalizedLocalActionSources(sources LocalActionSources) (LocalActionSources, error) {
	if sources == nil {
		return nil, nil
	}
	var node yaml.Node
	if err := node.Encode(sources); err != nil {
		return nil, err
	}
	var normalized LocalActionSources
	if err := node.Decode(&normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// UnmarshalYAML validates and normalizes explicit source mappings.
func (sources *LocalActionSources) UnmarshalYAML(node *yaml.Node) error {
	if node.ShortTag() == "!!null" {
		*sources = nil
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("local-actions must map action directories to source directories")
	}
	next := make(LocalActionSources, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.ShortTag() != "!!str" || value.ShortTag() != "!!str" {
			return errors.New("local-actions paths must be strings")
		}
		from, err := localaction.Normalize(key.Value)
		if err != nil {
			return err
		}
		to, err := localaction.Normalize(value.Value)
		if err != nil {
			return err
		}
		if _, exists := next[from]; exists {
			return fmt.Errorf("duplicate local-actions directory %q", from)
		}
		next[from] = to
	}
	*sources = next
	return nil
}
