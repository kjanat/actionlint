// Package source defines document revisions, stable node references and
// identities, and the Unicode column and LF line helpers they rely on.
package source

// Reference separates identity from an address in a particular document revision.
type Reference struct {
	Document string `json:"document"`
	Revision string `json:"revision"`
	Node     string `json:"node"`
	Address  string `json:"address"`
}
