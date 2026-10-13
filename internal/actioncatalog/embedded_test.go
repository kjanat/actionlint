//go:build !actionlint_external_catalog

package actioncatalog

import "testing"

func TestDefault(t *testing.T) {
	catalog := Default()
	if catalog.Version != 1 || len(catalog.References) == 0 || len(catalog.Definitions) == 0 || len(catalog.Retired) == 0 {
		t.Fatalf("incomplete bundled catalogue: version=%d references=%d definitions=%d retired=%d",
			catalog.Version, len(catalog.References), len(catalog.Definitions), len(catalog.Retired))
	}
}
