// Package rule defines declarations shared by built-in and plugin checks.
package rule

import (
	"encoding/json"
	"slices"
)

// InputKind identifies shared information a check requires from the host.
type InputKind string

const (
	Source       InputKind = "source"
	Scripts      InputKind = "scripts"
	Mappings     InputKind = "mappings"
	Invocations  InputKind = "invocations"
	Execution    InputKind = "execution"
	Dependencies InputKind = "dependencies"
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
	// Options constructs a fresh value for option decoding and schema generation.
	// Nil means the rule accepts no options.
	Options func() any
}

// Descriptor describes a check independently of its execution backend.
type Descriptor struct {
	// ExecutionFailure marks checks whose errors identify invalid syntax or unsupported execution.
	// Warning and info findings from the same check do not imply execution failure.
	ExecutionFailure bool `json:"execution_failure,omitempty"`
	// DefaultLevel is the severity used when enabled; empty selects the ordinary severity.
	DefaultLevel string `json:"default_level,omitempty"`
	// DefaultEnabled records the host's default selection when known.
	DefaultEnabled   *bool           `json:"default_enabled,omitempty"`
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
	// RequiredInputs names host facts needed before execution.
	RequiredInputs []InputKind `json:"required_inputs,omitempty"`
}

type Example struct {
	Description string `json:"description"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

// Clone isolates mutable descriptor payloads from consumers.
func (d Descriptor) Clone() Descriptor {
	if d.DefaultEnabled != nil {
		d.DefaultEnabled = new(*d.DefaultEnabled)
	}
	d.OptionsSchema = slices.Clone(d.OptionsSchema)
	d.Examples = slices.Clone(d.Examples)
	d.Aliases = slices.Clone(d.Aliases)
	d.RequiredInputs = slices.Clone(d.RequiredInputs)
	return d
}
