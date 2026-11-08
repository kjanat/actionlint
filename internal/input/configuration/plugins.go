package config

import (
	"errors"
	"fmt"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// PluginDeclarations lists the plugin dependencies requested by the project.
// An empty or null list disables plugins, including inherited declarations.
type PluginDeclarations []pluginref.Reference

// DecodeConfig accepts repository strings and explicit catalogue mappings.
func (p *PluginDeclarations) DecodeConfig(value any) error {
	if value == nil {
		*p = PluginDeclarations{}
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return errors.New("plugins must be a sequence")
	}
	next := make(PluginDeclarations, 0, len(list))
	seen := map[string]bool{}
	for i, entry := range list {
		ref, err := decodePluginDeclaration(entry)
		if err != nil {
			return binding.At(strconv.Itoa(i), err)
		}
		key := ref.Name()
		if ref.IsAlias() {
			key = ref.Registry() + "\n" + ref.Name()
		}
		if seen[key] {
			return binding.At(strconv.Itoa(i), fmt.Errorf("duplicate plugin declaration %q", ref.Name()))
		}
		seen[key] = true
		next = append(next, ref)
	}
	*p = next
	return nil
}

func decodePluginDeclaration(value any) (pluginref.Reference, error) {
	if selected, ok := value.(string); ok {
		return pluginref.Parse(selected)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return pluginref.Reference{}, errors.New("expected a repository string or a mapping with name, version, and registry")
	}
	values := map[string]string{}
	for key, value := range object {
		if key != "name" && key != "version" && key != "registry" && key != "source" && key != "sha256" {
			return pluginref.Reference{}, binding.At(key, fmt.Errorf("unknown plugin field %q", key))
		}
		selected, ok := value.(string)
		if !ok || selected == "" {
			return pluginref.Reference{}, binding.At(key, fmt.Errorf("plugin %s must be a nonempty string", key))
		}
		values[key] = selected
	}
	if source, exists := values["source"]; exists {
		if len(values) != 1 {
			return pluginref.Reference{}, errors.New("a direct plugin source cannot also declare name, version, registry, or sha256")
		}
		ref, err := pluginref.Parse(source)
		if err != nil {
			return pluginref.Reference{}, err
		}
		if ref.DirectSource() == "" {
			return pluginref.Reference{}, errors.New("plugin source requires a digest-pinned HTTPS or OCI artifact")
		}
		return ref, nil
	}
	if digest, ok := values["sha256"]; ok {
		return pluginref.NewCandidate(values["name"], values["version"], values["registry"], digest)
	}
	return pluginref.New(values["name"], values["version"], values["registry"])
}

// EncodeConfig emits concise strings unless a custom catalogue is required.
func (p PluginDeclarations) EncodeConfig() (any, error) {
	entries := make([]any, 0, len(p))
	for _, ref := range p {
		if ref.DirectSource() != "" || ref.Registry() == pluginref.DefaultRegistry {
			entries = append(entries, ref.String())
			continue
		}
		declaration := struct {
			Name     string `yaml:"name"`
			Version  string `yaml:"version,omitempty"`
			Registry string `yaml:"registry"`
			SHA256   string `yaml:"sha256,omitempty"`
		}{ref.Name(), ref.Version(), ref.Registry(), ref.CandidateDigest()}
		entries = append(entries, declaration)
	}
	return entries, nil
}
