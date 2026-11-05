// Package rule defines declarations shared by built-in and plugin checks.
package rule

import (
	"encoding/json"
	"slices"
)

// ConfigurationScope selects which declaration supplies a rule's configuration.
type ConfigurationScope uint8

const (
	CallerConfiguration ConfigurationScope = iota
	SourceConfiguration
	CompositeConfiguration
)

// Declaration combines a rule's metadata with its configuration requirements.
type Declaration struct {
	Descriptor
	SupportsActions bool
	Scope           ConfigurationScope
	// An empty DefaultLevel selects the ordinary enabled severity.
	DefaultLevel string
	// Options constructs a fresh value for option decoding and schema generation.
	// Nil means the rule accepts no options.
	Options func() any
}

// Descriptor describes a check independently of its execution backend.
type Descriptor struct {
	Name             string          `json:"name"`
	Aliases          []Alias         `json:"aliases,omitempty"`
	Description      string          `json:"description"`
	Category         string          `json:"category"`
	Subcategory      string          `json:"subcategory"`
	Maturity         string          `json:"maturity"`
	Provider         string          `json:"provider"`
	Recommended      bool            `json:"recommended"`
	Explanation      string          `json:"explanation"`
	Help             string          `json:"help"`
	DocumentationURL string          `json:"documentation_url"`
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
	d.Aliases = slices.Clone(d.Aliases)
	return d
}
