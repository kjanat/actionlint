package toolmanifest

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
)

func ResolveTransform(transform *Transform, programs func(string) ([]byte, bool)) (string, error) {
	if transform.Language != LanguageJQ {
		return "", fmt.Errorf("unsupported transform language %q", transform.Language)
	}
	if transform.Inline != "" {
		return transform.Inline, nil
	}
	if programs == nil {
		return "", fmt.Errorf("transform program %s is not available", transform.Digest)
	}
	source, ok := programs(transform.Digest)
	if !ok {
		return "", fmt.Errorf("transform program %s is not available", transform.Digest)
	}
	if "sha256:"+sha256hex.Digest(source) != transform.Digest {
		return "", fmt.Errorf("transform program does not match digest %s", transform.Digest)
	}
	return string(source), nil
}

func (m Manifest) Transforms() []*Transform {
	var found []*Transform
	candidates := []*Transform{m.ConfigurationTransform, m.PreparationTransform}
	if m.Process != nil {
		candidates = append(candidates, m.Process.Config.Transform, m.Process.Output.Transform)
	}
	for _, transform := range candidates {
		if transform != nil {
			found = append(found, transform)
		}
	}
	return found
}
