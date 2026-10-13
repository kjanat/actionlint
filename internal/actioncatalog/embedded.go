//go:build !actionlint_external_catalog

package actioncatalog

import _ "embed"

//go:embed popular_actions.json
var bundledData []byte

// Default loads the offline catalogue for ordinary CLI and library builds.
func Default() *Catalog {
	catalog, err := Decode(bundledData)
	if err != nil {
		panic("invalid bundled action catalogue: " + err.Error())
	}
	return catalog
}
