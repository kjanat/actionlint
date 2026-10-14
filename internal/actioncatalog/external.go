//go:build actionlint_external_catalog

package actioncatalog

// Default returns an empty catalogue for hosts that supply their own data.
func Default() *Catalog {
	return &Catalog{Version: 1}
}
