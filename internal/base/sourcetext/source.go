// Package sourcetext defines document revisions, stable node references and
// identities, and the Unicode column and LF line helpers they rely on.
package sourcetext

type Reference struct {
	Document string `json:"document"`
	Revision string `json:"revision"`
	Node     string `json:"node"`
	Address  string `json:"address"`
}
