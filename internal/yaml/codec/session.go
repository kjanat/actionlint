package codec

import (
	"fmt"
	"reflect"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "go.yaml.in/yaml/v4"
	"go.yaml.in/yaml/v4/plugin/limit"
)

// Decoder shares one source graph and failure state across nested decoding.
// It is valid only during the top-level call and must be used sequentially.
// Source nodes must not be mutated while that call is active.
type Decoder struct {
	graph                  parserGraph
	nodes                  map[*syntax.Node]*upstream.Node
	active                 map[hookVisit]bool
	fatal                  *LoadError
	closed                 bool
	aliases, constructions int
	seeded                 map[shadowIdentity]reflect.Value
}

type shadowIdentity struct {
	typeOf  reflect.Type
	pointer uintptr
}

func shadowID(value reflect.Value) shadowIdentity {
	return shadowIdentity{typeOf: value.Type(), pointer: value.Pointer()}
}

type hookVisit struct {
	typeOf reflect.Type
	node   *syntax.Node
}

type GraphError struct {
	Mark   Mark
	Reason string
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("YAML graph at %d:%d: %s", e.Mark.Line, e.Mark.Column, e.Reason)
}

func newDecoder(graph parserGraph) *Decoder {
	d := &Decoder{graph: graph, nodes: make(map[*syntax.Node]*upstream.Node, len(graph.originals)), active: map[hookVisit]bool{}, seeded: map[shadowIdentity]reflect.Value{}}
	for parsed, owned := range graph.originals {
		d.nodes[owned] = parsed
	}
	return d
}

type loadBudget struct {
	decoder                *Decoder
	defaults               *limit.Plugin
	aliases, constructions int
}

func (b *loadBudget) CheckDepth(depth int, ctx *upstream.DepthContext) error {
	return b.defaults.CheckDepth(depth, ctx)
}

func (b *loadBudget) CheckAlias(aliases, constructions int) error {
	b.decoder.aliases += aliases - b.aliases
	b.decoder.constructions += constructions - b.constructions
	b.aliases, b.constructions = aliases, constructions
	return b.defaults.CheckAlias(b.decoder.aliases, b.decoder.constructions)
}

func (d *Decoder) load(node *upstream.Node, target any) error {
	return node.Load(target, upstream.WithV3Defaults(), upstream.WithPlugin(&loadBudget{decoder: d, defaults: limit.New()}))
}
