// Package plugins reads and edits plugin declarations in configuration files.
package plugins

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/pluginreference"
)

// References reads the effective plugin list and reports whether it was declared.
func References(value any) ([]pluginreference.Reference, bool, error) {
	if err := ValidateRoot(value); err != nil {
		return nil, false, err
	}
	raw, exists := value.(map[string]any)["plugins"]
	if !exists {
		return nil, false, nil
	}
	var declarations configuration.PluginDeclarations
	if err := declarations.DecodeConfig(raw); err != nil {
		return nil, true, binding.At("plugins", err)
	}
	return []pluginreference.Reference(declarations), true, nil
}

func ValidateRoot(value any) error {
	if _, ok := value.(map[string]any); !ok {
		return errors.New("config must be a mapping")
	}
	return nil
}

func pluginManifestSequence(refs []pluginreference.Reference) ([]any, error) {
	sequence := make([]any, 0, len(refs))
	for _, ref := range refs {
		normalized, err := ref.Normalize()
		if err != nil {
			return nil, err
		}
		if normalized.DirectSource() != "" || normalized.Registry() == pluginreference.DefaultRegistry {
			sequence = append(sequence, normalized.String())
			continue
		}
		entry := map[string]any{"name": normalized.Name(), "version": normalized.Version(), "registry": normalized.Registry()}
		if digest := normalized.CandidateDigest(); digest != "" {
			entry["sha256"] = digest
		}
		sequence = append(sequence, entry)
	}
	return sequence, nil
}

// EditManifest returns original with its plugins setting replaced by refs.
// It checks that the edit changes no other setting and declares exactly refs.
func EditManifest(path string, original []byte, refs []pluginreference.Reference) ([]byte, error) {
	editor, err := document.NewEditor(path, original)
	if err != nil {
		return nil, err
	}
	before := editor.Root().Value()
	if err = ValidateRoot(before); err != nil {
		return nil, err
	}
	sequence, err := pluginManifestSequence(refs)
	if err != nil {
		return nil, err
	}
	edited, err := editor.SetList("plugins", sequence, pluginTableFields)
	if err != nil {
		return nil, err
	}
	after, err := document.Parse(path, edited)
	if err != nil {
		return nil, fmt.Errorf("cannot safely edit plugin config: %w", err)
	}
	if !reflect.DeepEqual(withoutManifestPlugins(before), withoutManifestPlugins(after.Value())) {
		return nil, errors.New("cannot edit plugins without changing other config settings")
	}
	actual, declared, err := References(after.Value())
	if err != nil {
		return nil, fmt.Errorf("invalid edited plugin declarations: %w", err)
	}
	if !declared {
		return nil, errors.New("edited config contains no plugin declarations")
	}
	expected, _, err := References(map[string]any{"plugins": sequence})
	if err != nil {
		return nil, err
	}
	if !slices.Equal(actual, expected) {
		return nil, errors.New("edited plugin declarations differ from requested plugins")
	}
	return edited, nil
}

func withoutManifestPlugins(value any) any {
	m, ok := value.(map[string]any)
	if !ok {
		return value
	}
	next := maps.Clone(m)
	delete(next, "plugins")
	return next
}
