// Package actioncatalog defines the versioned offline action catalogue format.
package actioncatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Catalog separates exact references from deduplicated validation definitions.
type Catalog struct {
	Schema      string                `json:"$schema"`
	Version     int                   `json:"version"`
	References  map[string]string     `json:"references"`
	Definitions map[string]Definition `json:"definitions"`
	Retired     []string              `json:"retired"`
}

// Definition contains only metadata used by remote-action validation.
// Display names use source spelling; lookup identifiers are lowercase.
type Definition struct {
	Name        string            `json:"name"`
	Runtime     string            `json:"runtime,omitempty"`
	Inputs      []string          `json:"inputs,omitempty"`
	Required    []string          `json:"required,omitempty"`
	Deprecated  map[string]string `json:"deprecated,omitempty"`
	Outputs     []string          `json:"outputs,omitempty"`
	SkipInputs  bool              `json:"skipInputs,omitempty"`
	SkipOutputs bool              `json:"skipOutputs,omitempty"`
}

// Decode rejects unsupported versions and broken cross-references.
func Decode(data []byte) (*Catalog, error) {
	var c Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing data in action catalogue")
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("unsupported action catalogue version %d", c.Version)
	}
	if c.References == nil || c.Definitions == nil || c.Retired == nil {
		return nil, errors.New("action catalogue needs references, definitions and retired")
	}
	for ref, id := range c.References {
		if _, ok := c.Definitions[id]; !ok {
			return nil, fmt.Errorf("reference %q uses missing definition %q", ref, id)
		}
	}
	for _, ref := range c.Retired {
		if _, ok := c.References[ref]; ok {
			return nil, fmt.Errorf("reference %q is both active and retired", ref)
		}
	}
	for id, d := range c.Definitions {
		if d.Name == "" {
			return nil, fmt.Errorf("definition %q has no name", id)
		}
		inputs := make(map[string]bool)
		for _, name := range d.Inputs {
			key := strings.ToLower(name)
			if name == "" || inputs[key] {
				return nil, fmt.Errorf("definition %q has empty or duplicate input %q", id, name)
			}
			inputs[key] = true
		}
		for _, key := range d.Required {
			if !inputs[key] {
				return nil, fmt.Errorf("definition %q requires unknown input %q", id, key)
			}
		}
		for key := range d.Deprecated {
			if !inputs[key] {
				return nil, fmt.Errorf("definition %q deprecates unknown input %q", id, key)
			}
		}
		outputs := make(map[string]bool)
		for _, name := range d.Outputs {
			key := strings.ToLower(name)
			if name == "" || outputs[key] {
				return nil, fmt.Errorf("definition %q has empty or duplicate output %q", id, name)
			}
			outputs[key] = true
		}
	}
	return &c, nil
}
