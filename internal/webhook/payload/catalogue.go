// Package payload provides versioned webhook observations without interpreting
// workflow triggers, expression types, or lint policy.
package payload

import (
	_ "embed"
	"encoding/json/v2"
	"maps"
	"slices"
	"sync"
)

//go:generate node generate/main.mts
//go:embed catalogue.json
var catalogueJSON []byte

type Property struct {
	Type        int    `json:"type"`
	Optional    bool   `json:"optional"`
	Description string `json:"description"`
}
type Shape struct {
	Kind         string              `json:"kind"`
	Properties   map[string]Property `json:"properties"`
	Element      int                 `json:"element"`
	Mapped       *int                `json:"mapped"`
	Alternatives []int               `json:"alternatives"`
	Literal      string              `json:"literal"`
	Required     []string            `json:"required"`
	// AdditionalProperties records the explicit source policy; empty means the
	// schema default (allowed), independently of the Actions lookup projection.
	AdditionalProperties string `json:"additionalProperties"`
}

// Provenance records the input actually used to generate these observations.
type Provenance struct {
	Package string `json:"package"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
}

type catalogue struct {
	Source Provenance                `json:"source"`
	Events map[string]map[string]int `json:"events"`
	Nodes  []Shape                   `json:"nodes"`
}

var data = sync.OnceValue(func() catalogue {
	var c catalogue
	if err := json.Unmarshal(catalogueJSON, &c); err != nil {
		panic(err)
	}
	return c
})

// Variants returns an isolated event/action index. Names are upstream names;
// Actions aliases belong to the workflow adapter.
func Variants(event string) (map[string]int, bool) {
	variants, ok := data().Events[event]
	return maps.Clone(variants), ok
}

// Node returns an isolated observation, preserving optionality, descriptions and
// literal values. Consumers decide how to interpret those observations.
func Node(id int) Shape {
	n := data().Nodes[id]
	n.Properties = maps.Clone(n.Properties)
	n.Alternatives = slices.Clone(n.Alternatives)
	n.Required = slices.Clone(n.Required)
	if n.Mapped != nil {
		n.Mapped = new(*n.Mapped)
	}
	return n
}

// Source reports catalogue provenance independently of dependency constraints.
func Source() Provenance { return data().Source }
