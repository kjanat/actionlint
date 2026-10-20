package config

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/internal/pluginref"
	"go.yaml.in/yaml/v4"
)

// PluginDeclarations lists the plugin dependencies requested by the project.
// An empty or null list disables plugins, including inherited declarations.
type PluginDeclarations []pluginref.Reference

// UnmarshalYAML accepts repository strings and explicit catalogue mappings.
func (p *PluginDeclarations) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null" {
		*p = PluginDeclarations{}
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("plugins must be a sequence at line:%d,col:%d", node.Line, node.Column)
	}
	next := make(PluginDeclarations, 0, len(node.Content))
	seen := map[string]bool{}
	for _, entry := range node.Content {
		ref, err := decodePluginDeclaration(entry)
		if err != nil {
			return fmt.Errorf("plugins entry at line:%d,col:%d: %w", entry.Line, entry.Column, err)
		}
		if seen[ref.Name] {
			return fmt.Errorf("duplicate plugin repository %q at line:%d,col:%d", ref.Name, entry.Line, entry.Column)
		}
		seen[ref.Name] = true
		next = append(next, ref)
	}
	*p = next
	return nil
}

func decodePluginDeclaration(node *yaml.Node) (pluginref.Reference, error) {
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		return pluginref.Parse(node.Value)
	}
	if node.Kind != yaml.MappingNode {
		return pluginref.Reference{}, errors.New("expected a repository string or a mapping with name, version, and registry")
	}
	values := map[string]string{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.ShortTag() != "!!str" || (key.Value != "name" && key.Value != "version" && key.Value != "registry") {
			return pluginref.Reference{}, fmt.Errorf("unknown plugin field %q", key.Value)
		}
		if _, exists := values[key.Value]; exists {
			return pluginref.Reference{}, fmt.Errorf("duplicate plugin field %q", key.Value)
		}
		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" || value.Value == "" {
			return pluginref.Reference{}, fmt.Errorf("plugin %s must be a nonempty string", key.Value)
		}
		values[key.Value] = value.Value
	}
	return pluginref.New(values["name"], values["version"], values["registry"])
}

// MarshalYAML emits concise strings unless a custom catalogue is required.
func (p PluginDeclarations) MarshalYAML() (any, error) {
	entries := make([]any, 0, len(p))
	for _, ref := range p {
		if ref.Registry == pluginref.DefaultRegistry {
			entries = append(entries, ref.String())
			continue
		}
		entries = append(entries, struct {
			Name     string `yaml:"name"`
			Version  string `yaml:"version,omitempty"`
			Registry string `yaml:"registry"`
		}{ref.Name, ref.Version, ref.Registry})
	}
	return entries, nil
}
