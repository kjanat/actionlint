//go:build actionlint_external_catalog

package actioncatalog

import "testing"

func TestDefault(t *testing.T) {
	catalog := Default()
	if catalog.Version != 1 || len(catalog.References) != 0 || len(catalog.Definitions) != 0 || len(catalog.Retired) != 0 {
		t.Fatalf("external build must start with an empty version 1 catalogue: %+v", catalog)
	}
}
