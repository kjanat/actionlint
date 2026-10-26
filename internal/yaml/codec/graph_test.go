package codec

import (
	"bytes"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "go.yaml.in/yaml/v4"
)

func TestEncoderRejectsInvalidGraphBeforeWriting(t *testing.T) {
	anchor := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "value", Anchor: "a"}
	for _, invalid := range []*syntax.Node{
		{Kind: syntax.AliasNode, Value: "a", Alias: anchor},
		{Kind: syntax.SequenceNode, Content: []*syntax.Node{{Kind: syntax.AliasNode, Value: "a", Alias: anchor}, anchor}},
		{Kind: syntax.SequenceNode, Content: []*syntax.Node{anchor, {Kind: syntax.AliasNode, Value: "different", Alias: anchor}}},
		{Kind: syntax.ScalarNode, Value: "value", Anchor: "bad anchor"},
	} {
		var output bytes.Buffer
		encoder := NewEncoder(&output)
		if err := encoder.Encode("first"); err != nil {
			t.Fatal(err)
		}
		before := output.String()
		err := encoder.Encode(invalid)
		if _, ok := errors.AsType[*EncodeError](err); !ok {
			t.Fatalf("invalid graph error = %v", err)
		}
		if output.String() != before {
			t.Fatalf("rejected graph wrote bytes: %q", output.String())
		}
		if err := encoder.Encode("last"); err != nil {
			t.Fatal(err)
		}
		if err := encoder.Close(); err != nil {
			t.Fatal(err)
		}
		decoder := upstream.NewDecoder(bytes.NewReader(output.Bytes()))
		for _, want := range []string{"first", "last"} {
			var got string
			if err := decoder.Decode(&got); err != nil || got != want {
				t.Fatalf("stream decoded %q, %v; want %q", got, err, want)
			}
		}
	}
}

func TestGraphRejectsInvalidStructure(t *testing.T) {
	cycle := &syntax.Node{Kind: syntax.SequenceNode}
	cycle.Content = []*syntax.Node{cycle}
	for _, node := range []*syntax.Node{
		{Value: "untyped"},
		{Kind: syntax.SequenceNode, Content: []*syntax.Node{{}}},
		{Kind: syntax.SequenceNode, Content: []*syntax.Node{nil}},
		{Kind: syntax.AliasNode, Value: "missing"},
		{Kind: syntax.ScalarNode, Alias: &syntax.Node{Kind: syntax.ScalarNode}},
		{Kind: syntax.MappingNode, Content: []*syntax.Node{{Kind: syntax.ScalarNode}}},
		cycle,
	} {
		if _, err := Marshal(node); err == nil {
			t.Errorf("accepted invalid graph with root kind %v", node.Kind)
		}
	}
	if _, err := Marshal(&syntax.Node{}); err != nil {
		t.Fatalf("zero root: %v", err)
	}
}

func TestRecursiveAliasGraphEncodesWithoutExpanding(t *testing.T) {
	var node syntax.Node
	if err := Unmarshal([]byte("&a [*a]\n"), &node); err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(&node)
	if err != nil || string(encoded) != "&a [*a]\n" {
		t.Fatalf("recursive graph encoded as %q, %v", encoded, err)
	}
	if _, err := toParser(node.Content[0].Content[0]); err != nil {
		t.Fatalf("standalone alias view: %v", err)
	}
}

func TestKindAndStyleMappings(t *testing.T) {
	for _, pair := range kinds {
		gotParser, parserErr := parserKind(pair.owned)
		gotOwned, ownedErr := ownedKind(pair.parser)
		if parserErr != nil || ownedErr != nil || gotParser != pair.parser || gotOwned != pair.owned {
			t.Fatalf("kind pair %+v: %v, %v, %v, %v", pair, gotParser, gotOwned, parserErr, ownedErr)
		}
	}
	if _, err := ownedKind(upstream.StreamNode); err == nil {
		t.Fatal("stream accepted as document node")
	}
	for mask := range 1 << len(styles) {
		var wantOwned syntax.Style
		var wantParser upstream.Style
		for i, pair := range styles {
			if mask&(1<<i) != 0 {
				wantOwned |= pair.owned
				wantParser |= pair.parser
			}
		}
		gotParser, parserErr := parserStyle(wantOwned)
		gotOwned, ownedErr := ownedStyle(wantParser)
		if parserErr != nil || ownedErr != nil || gotParser != wantParser || gotOwned != wantOwned {
			t.Fatalf("style subset %d: %v, %v, %v, %v", mask, gotParser, gotOwned, parserErr, ownedErr)
		}
	}
}
