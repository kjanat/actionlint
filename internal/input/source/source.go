// Package source defines document revisions and stable node references.
package source

// Reference separates identity from an address in a particular document revision.
type Reference struct {
	Document string `json:"document"`
	Revision string `json:"revision"`
	Node     string `json:"node"`
	Address  string `json:"address"`
}
