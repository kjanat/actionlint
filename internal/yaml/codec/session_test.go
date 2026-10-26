package codec_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type retainedDecoderHook struct {
	decoder *codec.Decoder
	node    *syntax.Node
}

func (h *retainedDecoderHook) UnmarshalYAML(d *codec.Decoder, node *syntax.Node) error {
	h.decoder = d
	h.node = node
	return nil
}

func TestDecoderRejectsUseAfterInvocation(t *testing.T) {
	var root syntax.Node
	if err := codec.Unmarshal([]byte("hook: value\n"), &root); err != nil {
		t.Fatalf("parse owned graph: %v", err)
	}
	var hook retainedDecoderHook
	if err := codec.Decode(&root, &hook); err != nil {
		t.Fatalf("codec.Decode: %v", err)
	}
	if hook.decoder == nil || hook.node == nil {
		t.Fatal("hook did not receive the invocation decoder and source node")
	}

	var value string
	err := hook.decoder.Decode(hook.node, &value)
	if _, ok := errors.AsType[*codec.GraphError](err); !ok {
		t.Fatalf("closed decoder error = %T %v, want *codec.GraphError", err, err)
	}
}

func TestZeroDecoderRejectsDecode(t *testing.T) {
	var decoder codec.Decoder
	node := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "value", Line: 1, Column: 1}
	var value string
	err := decoder.Decode(node, &value)
	if _, ok := errors.AsType[*codec.GraphError](err); !ok {
		t.Fatalf("zero decoder error = %T %v, want *codec.GraphError", err, err)
	}
}

type foreignNodeHook struct{ err error }

func (h *foreignNodeHook) UnmarshalYAML(d *codec.Decoder, _ *syntax.Node) error {
	node := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "outside", Line: 8, Column: 4}
	var value string
	h.err = d.Decode(node, &value)
	return h.err
}

func TestDecoderRejectsNodeOutsideInvocationGraph(t *testing.T) {
	var root syntax.Node
	if err := codec.Unmarshal([]byte("hook: value\n"), &root); err != nil {
		t.Fatalf("parse owned graph: %v", err)
	}
	var hook foreignNodeHook
	err := codec.Decode(&root, &hook)
	var graphErr *codec.GraphError
	if !errors.As(err, &graphErr) || !errors.As(hook.err, &graphErr) {
		t.Fatalf("foreign node errors: outer=%T %v nested=%T %v; want *codec.GraphError", err, err, hook.err, hook.err)
	}
}

type recursiveSessionHook struct{}

func (*recursiveSessionHook) UnmarshalYAML(d *codec.Decoder, node *syntax.Node) error {
	var nested struct {
		Again *recursiveSessionHook `yaml:"x"`
	}
	return d.Decode(node, &nested)
}

func TestDecoderStopsRecursiveHookAliasWithLocatedError(t *testing.T) {
	var root syntax.Node
	if err := codec.Unmarshal([]byte("&a {x: *a}\n"), &root); err != nil {
		t.Fatalf("parse recursive alias graph: %v", err)
	}
	node := root.Content[0]
	var hook recursiveSessionHook
	err := codec.Decode(&root, &hook)
	var loadErr *codec.LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("recursive hook error = %T %v, want located *codec.LoadError", err, err)
	}
	if loadErr.Mark.Line != node.Line || loadErr.Mark.Column != node.Column || loadErr.Mark.Line == 0 {
		t.Errorf("recursive hook mark = %+v, source node at %d:%d", loadErr.Mark, node.Line, node.Column)
	}
}

const aliasBudgetItems = 3
const aliasBudgetDepth = 3
const aliasBudgetWidth = 8

var aliasBudgetHookCalls int

type aliasBudgetHook struct{ Values map[string]any }

func (h *aliasBudgetHook) UnmarshalYAML(d *codec.Decoder, node *syntax.Node) error {
	aliasBudgetHookCalls++
	return d.Decode(node, &h.Values)
}

type aliasBudgetTarget struct {
	Items []aliasBudgetHook `yaml:"items"`
}

type upstreamAliasBudgetTarget struct {
	Items []map[string]any `yaml:"items"`
}

func splitAliasBudgetSource() []byte {
	var source strings.Builder
	source.WriteString("a0: &a0 value\n")
	for level := 1; level <= aliasBudgetDepth; level++ {
		fmt.Fprintf(&source, "a%d: &a%d [", level, level)
		for entry := range aliasBudgetWidth {
			if entry > 0 {
				source.WriteString(", ")
			}
			fmt.Fprintf(&source, "*a%d", level-1)
		}
		source.WriteString("]\n")
	}
	source.WriteString("items:\n")
	for range aliasBudgetItems {
		fmt.Fprintf(&source, "  - {value: *a%d}\n", aliasBudgetDepth)
	}
	return []byte(source.String())
}

func TestNestedHooksShareAliasExpansionBudgetAndStopAfterFatal(t *testing.T) {
	data := splitAliasBudgetSource()
	var baseline upstreamAliasBudgetTarget
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want its document-wide alias limit", wantErr)
	}

	var root syntax.Node
	if err := codec.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse owned graph: %v", err)
	}
	items := mappingValue(&root, "items")
	anchor := mappingValue(&root, fmt.Sprintf("a%d", aliasBudgetDepth))
	if items == nil || anchor == nil || len(items.Content) != aliasBudgetItems {
		t.Fatalf("source alias graph has items=%#v anchor=%#v", items, anchor)
	}
	aliases := make([]*syntax.Node, 0, aliasBudgetItems)
	for _, item := range items.Content {
		alias := mappingValue(item, "value")
		if alias == nil || alias.Kind != syntax.AliasNode || alias.Alias != anchor {
			t.Fatalf("source occurrence = %#v, want alias to anchor %p", alias, anchor)
		}
		aliases = append(aliases, alias)
	}
	aliasBudgetHookCalls = 0
	var got aliasBudgetTarget
	err := codec.Decode(&root, &got)
	if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("codec alias expansion error = %v, want upstream's document-wide alias limit", err)
	}
	if aliasBudgetHookCalls == 0 || aliasBudgetHookCalls >= aliasBudgetItems {
		t.Errorf("hook calls before fatal limit = %d, want some calls and no hooks after the limit", aliasBudgetHookCalls)
	}
	for i, alias := range aliases {
		if alias.Kind != syntax.AliasNode || alias.Alias != anchor {
			t.Errorf("source alias occurrence %d changed during decoding: %#v", i, alias)
		}
	}
}

const inheritedAliasWidth = 650
const inheritedAliasOccurrences = 3

type inheritedAliasHook struct{ Values []string }

func (h *inheritedAliasHook) UnmarshalYAML(d *codec.Decoder, node *syntax.Node) error {
	return d.Decode(node, &h.Values)
}

type inheritedAliasTarget struct {
	Items [][]string `yaml:"items"`
}

type inheritedAliasHookTarget struct {
	Items []inheritedAliasHook `yaml:"items"`
}

func inheritedAliasSource() []byte {
	var source strings.Builder
	source.WriteString("payload: &payload [")
	for i := range inheritedAliasWidth {
		if i > 0 {
			source.WriteString(", ")
		}
		source.WriteString("value")
	}
	source.WriteString("]\nitems:\n")
	for range inheritedAliasOccurrences {
		source.WriteString("  - *payload\n")
	}
	return []byte(source.String())
}

func TestNestedHookKeepsAliasBudgetFromCapturedOccurrence(t *testing.T) {
	data := inheritedAliasSource()
	var baseline inheritedAliasTarget
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want document-wide alias limit", wantErr)
	}
	t.Logf("upstream baseline: %v", wantErr)

	var root syntax.Node
	if err := codec.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse owned graph: %v", err)
	}
	items := mappingValue(&root, "items")
	anchor := mappingValue(&root, "payload")
	if items == nil || anchor == nil || len(items.Content) != inheritedAliasOccurrences {
		t.Fatalf("source graph has items=%#v anchor=%#v", items, anchor)
	}
	aliases := make([]*syntax.Node, 0, inheritedAliasOccurrences)
	for _, occurrence := range items.Content {
		if occurrence.Kind != syntax.AliasNode || occurrence.Alias != anchor {
			t.Fatalf("source occurrence = %#v, want original alias to payload %p", occurrence, anchor)
		}
		aliases = append(aliases, occurrence)
	}

	var got inheritedAliasHookTarget
	err := codec.Decode(&root, &got)
	if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Errorf("hooked codec error = %v, want upstream's alias limit inherited across captured aliases", err)
	}
	for i, alias := range aliases {
		if alias.Kind != syntax.AliasNode || alias.Alias != anchor {
			t.Errorf("source alias occurrence %d changed during decode: %#v", i, alias)
		}
	}
}
