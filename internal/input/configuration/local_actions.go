package config

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	"actionlint.kjanat.dev/v2/internal/model/actions/reference"
)

// LocalActionSources maps generated local action directories to source directories.
type LocalActionSources map[string]string

// Canonical keys make equivalent ./ and $/ aliases override each other during extends merging.
// Each source document has already been validated before this function runs.
func normalizeLocalActionSourceNode(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, value := range object {
		if normalized, err := reference.NormalizeLocalDirectory(key); err == nil && normalized != key {
			delete(object, key)
			object[normalized] = value
		}
	}
}

func normalizedLocalActionSources(sources LocalActionSources) (LocalActionSources, error) {
	if sources == nil {
		return nil, nil
	}
	value, err := binding.Encode(sources)
	if err != nil {
		return nil, err
	}
	var result LocalActionSources
	if err := result.DecodeConfig(value); err != nil {
		return nil, err
	}
	return result, nil
}

// DecodeConfig validates and normalizes explicit source mappings.
func (sources *LocalActionSources) DecodeConfig(value any) error {
	if value == nil {
		*sources = nil
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("local-actions must map action directories to source directories")
	}
	next := make(LocalActionSources, len(object))
	for key, value := range object {
		selected, ok := value.(string)
		if !ok {
			return binding.At(key, errors.New("local-actions paths must be strings"))
		}
		from, err := reference.NormalizeLocalDirectory(key)
		if err != nil {
			return binding.At(key, err)
		}
		to, err := reference.NormalizeLocalDirectory(selected)
		if err != nil {
			return binding.At(key, err)
		}
		if _, exists := next[from]; exists {
			return binding.At(key, fmt.Errorf("duplicate local-actions directory %q", from))
		}
		next[from] = to
	}
	*sources = next
	return nil
}
