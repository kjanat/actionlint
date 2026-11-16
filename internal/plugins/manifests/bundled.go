package manifests

import (
	"fmt"
	"sync"

	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
)

// Bundled returns every provider shipped inside actionlint in directory order.
// Each manifest's rule name must equal its directory and each transform
// digest must name a bundled program. The embedded data is decoded once per
// process; each call returns copies the caller owns.
func Bundled() ([]Manifest, error) {
	decoded, err := bundled()
	if err != nil {
		return nil, err
	}
	manifests := make([]Manifest, len(decoded))
	for i, manifest := range decoded {
		manifests[i] = manifest.Clone()
	}
	return manifests, nil
}

var bundled = sync.OnceValues(func() ([]Manifest, error) {
	names := bundledproviders.Names()
	manifests := make([]Manifest, 0, len(names))
	for _, name := range names {
		data, ok := bundledproviders.Manifest(name)
		if !ok {
			return nil, fmt.Errorf("bundled provider %s has no manifest", name)
		}
		manifest, err := ParsePluginManifest(data)
		if err != nil {
			return nil, fmt.Errorf("bundled provider %s: %w", name, err)
		}
		if manifest.Rule.Name != name {
			return nil, fmt.Errorf("bundled provider %s declares rule %q", name, manifest.Rule.Name)
		}
		for _, transform := range manifest.Transforms() {
			if transform.Digest == "" {
				continue
			}
			if _, ok := bundledproviders.Program(transform.Digest); !ok {
				return nil, fmt.Errorf("bundled provider %s references missing program %s", name, transform.Digest)
			}
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
})
