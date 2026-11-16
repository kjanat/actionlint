package configuration

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
)

type enforcedLifecycleFile struct {
	Extends []string `yaml:"extends"`
	Lint    struct {
		Lifecycle LifecycleConfig `yaml:"lifecycle"`
	} `yaml:"lint"`
}

// EnforcedLifecyclePolicies holds policies loaded independently of project settings.
type EnforcedLifecyclePolicies struct {
	policies []lifecycle.Policy
	files    []string
}

// Policies returns the policies from all specified configuration chains.
func (p *EnforcedLifecyclePolicies) Policies() []lifecycle.Policy {
	if p == nil {
		return nil
	}
	return slices.Clone(p.policies)
}

// Files returns the configuration files used to load the policies.
func (p *EnforcedLifecyclePolicies) Files() []string {
	if p == nil {
		return nil
	}
	return slices.Clone(p.files)
}

// ReadEnforcedLifecyclePolicies loads each explicit policy chain using read.
func ReadEnforcedLifecyclePolicies(paths []string, read func(string) ([]byte, error)) (*EnforcedLifecyclePolicies, error) {
	result := &EnforcedLifecyclePolicies{}
	for _, path := range paths {
		loaded, err := LoadSources(path, read, func(_ string, source *document.Node) (*document.Node, error) {
			object, ok := source.Value().(map[string]any)
			if !ok {
				return source, errors.New("enforced lifecycle policy must be an object")
			}
			object = maps.Clone(object)
			if value, exists := object["$schema"]; exists {
				if _, ok := value.(string); !ok {
					return source, errors.New("$schema must be a string")
				}
				delete(object, "$schema")
			}
			var file enforcedLifecycleFile
			if err := binding.Decode(object, &file); err != nil {
				return source, sourceError(err, source.Origins())
			}
			return document.Capture(object, source.Origins()), nil
		})
		if err != nil {
			return nil, err
		}
		var file enforcedLifecycleFile
		if err := binding.Decode(loaded.Root.Value(), &file); err != nil {
			return nil, fmt.Errorf("enforced lifecycle policy %q: %w", path, err)
		}
		if len(file.Lint.Lifecycle.Policies) == 0 {
			return nil, fmt.Errorf("enforced lifecycle policy %q contains no policies", path)
		}
		policies, err := file.Lint.Lifecycle.compile(path, loaded.Root.Origins(), "/lint/lifecycle/policies")
		if err != nil {
			return nil, fmt.Errorf("enforced lifecycle policy %q: %w", path, err)
		}
		result.policies = append(result.policies, policies...)
		result.files = append(result.files, loaded.Files...)
	}
	result.files = slices.Compact(slices.Sorted(slices.Values(result.files)))
	return result, nil
}
