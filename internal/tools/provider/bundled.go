package provider

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
)

// Bundled parses every provider shipped inside actionlint in directory order.
// Each manifest's rule name must equal its directory and each transform
// digest must name a bundled program.
func Bundled() ([]Manifest, error) {
	names := bundled.Names()
	manifests := make([]Manifest, 0, len(names))
	for _, name := range names {
		data, ok := bundled.Manifest(name)
		if !ok {
			return nil, fmt.Errorf("bundled provider %s has no manifest", name)
		}
		manifest, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("bundled provider %s: %w", name, err)
		}
		if manifest.Rule.Name != name {
			return nil, fmt.Errorf("bundled provider %s declares rule %q", name, manifest.Rule.Name)
		}
		for _, transform := range manifest.transforms() {
			if transform.Digest == "" {
				continue
			}
			if _, ok := bundled.Program(transform.Digest); !ok {
				return nil, fmt.Errorf("bundled provider %s references missing program %s", name, transform.Digest)
			}
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

// transforms lists the transforms a manifest references.
func (m Manifest) transforms() []*Transform {
	if m.Process == nil {
		return nil
	}
	var found []*Transform
	for _, transform := range []*Transform{m.Process.Config.Transform, m.Process.Output.Transform} {
		if transform != nil {
			found = append(found, transform)
		}
	}
	return found
}
