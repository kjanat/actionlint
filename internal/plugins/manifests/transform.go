package manifests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ResolveTransform returns a transform's text, verifying a digest-named program
// against its digest.
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
	sum := sha256.Sum256(source)
	if "sha256:"+hex.EncodeToString(sum[:]) != transform.Digest {
		return "", fmt.Errorf("transform program does not match digest %s", transform.Digest)
	}
	return string(source), nil
}

// Transforms lists the transforms a manifest references.
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
