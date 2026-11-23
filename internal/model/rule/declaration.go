// Package rule defines declarations shared by built-in and plugin checks.
package rule

import (
	"encoding/json"
	"slices"
)

type InputKind string

const (
	Source       InputKind = "source"
	Scripts      InputKind = "scripts"
	Mappings     InputKind = "mappings"
	Invocations  InputKind = "invocations"
	Execution    InputKind = "execution"
	Dependencies InputKind = "dependencies"
)

type ConfigurationScope uint8

const (
	CallerConfiguration ConfigurationScope = iota
	SourceConfiguration
	CompositeConfiguration
)

type Declaration struct {
	Descriptor
	SupportsActions bool
	Scope           ConfigurationScope
	// Options returns a fresh value on each call, and nil Options means the rule accepts no options.
	Options func() any `clone:"shared"`
	// An empty LegacyGroup means the rule's category.
	LegacyGroup string
	// The rule's level and options replace the Policy setting and still project into it.
	Policy       string
	MovedOptions []MovedOption
	// MigrationNote explains a change of meaning to users whose legacy
	// configuration selects the rule's name.
	MigrationNote string
}

func (d Declaration) Clone() Declaration {
	d.Descriptor = d.Descriptor.Clone()
	d.MovedOptions = slices.Clone(d.MovedOptions)
	return d
}

type MovedOption struct {
	Option     string
	Repository string
}

type Descriptor struct {
	// ExecutionFailure marks checks whose errors identify invalid syntax or unsupported execution.
	ExecutionFailure bool `json:"execution_failure,omitempty"`
	// An empty DefaultLevel selects the ordinary severity.
	DefaultLevel     string          `json:"default_level,omitempty"`
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
	RequiredInputs   []InputKind     `json:"required_inputs,omitempty"`
}

type Example struct {
	Description string `json:"description"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

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
