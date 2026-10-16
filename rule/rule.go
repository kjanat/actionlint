// Package rule defines rule metadata shared by native and plugin analyzers.
package rule

import (
	"encoding/json"
	"slices"
)

// Descriptor describes a check independently of its execution backend.
type Descriptor struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Category         string          `json:"category"`
	Subcategory      string          `json:"subcategory"`
	Maturity         string          `json:"maturity"`
	Provider         string          `json:"provider"`
	Recommended      bool            `json:"recommended"`
	Explanation      string          `json:"explanation"`
	Help             string          `json:"help"`
	DocumentationURL string          `json:"documentation_url"`
	FixAvailability  string          `json:"fix_availability"`
	OptionsSchema    json.RawMessage `json:"options_schema,omitempty"`
	Examples         []Example       `json:"examples,omitempty"`
}

type Example struct {
	Description string `json:"description"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

// Clone isolates mutable descriptor payloads from consumers.
func (d Descriptor) Clone() Descriptor {
	d.OptionsSchema = slices.Clone(d.OptionsSchema)
	d.Examples = slices.Clone(d.Examples)
	return d
}
