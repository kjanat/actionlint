package codec

import (
	"bytes"
	"errors"
	"testing"

	upstream "github.com/kjanat/go-yaml/v4"
)

func TestEncoderRejectsInvalidGraphBeforeWriting(t *testing.T) {
	t.Parallel()
	anchor := &Node{Kind: ScalarNode, Tag: "!!str", Value: "value", Anchor: "a"}
	for _, invalid := range []*Node{
		{Kind: AliasNode, Value: "a", Alias: anchor},
		{Kind: SequenceNode, Content: []*Node{{Kind: AliasNode, Value: "a", Alias: anchor}, anchor}},
		{Kind: SequenceNode, Content: []*Node{anchor, {Kind: AliasNode, Value: "different", Alias: anchor}}},
		{Kind: ScalarNode, Value: "value", Anchor: "bad anchor"},
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
	t.Parallel()
	cycle := &Node{Kind: SequenceNode}
	cycle.Content = []*Node{cycle}
	aliasCycle := &Node{Kind: AliasNode, Line: 5, Column: 3}
	aliasCycle.Alias = aliasCycle
	for _, node := range []*Node{
		{Value: "untyped"},
		{Kind: SequenceNode, Content: []*Node{{}}},
		{Kind: SequenceNode, Content: []*Node{nil}},
		{Kind: AliasNode, Value: "missing"},
		{Kind: ScalarNode, Alias: &Node{Kind: ScalarNode}},
		{Kind: MappingNode, Content: []*Node{{Kind: ScalarNode}}},
		cycle,
		aliasCycle,
	} {
		if _, err := Marshal(node); err == nil {
			t.Errorf("accepted invalid graph with root kind %v", node.Kind)
		}
		if session, _, err := Open(node); err == nil {
			session.Close()
			t.Errorf("opened invalid graph with root kind %v", node.Kind)
		}
	}
	if _, err := Marshal(&Node{}); err != nil {
		t.Fatalf("zero root: %v", err)
	}
}

func TestRecursiveAliasGraphEncodesWithoutExpanding(t *testing.T) {
	t.Parallel()
	node, err := ParseYAML([]byte("&a [*a]\n"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(node)
	if err != nil || string(encoded) != "&a [*a]\n" {
		t.Fatalf("recursive graph encoded as %q, %v", encoded, err)
	}
	if _, err := toParser(node.Content[0].Content[0]); err != nil {
		t.Fatalf("standalone alias view: %v", err)
	}
}

func TestKindAndStyleMappings(t *testing.T) {
	t.Parallel()
	for _, pair := range kinds {
		gotParser, parserErr := parserKind(pair.codec)
		gotCodec, codecErr := codecKind(pair.parser)
		if parserErr != nil || codecErr != nil || gotParser != pair.parser || gotCodec != pair.codec {
			t.Fatalf("kind pair %+v: %v, %v, %v, %v", pair, gotParser, gotCodec, parserErr, codecErr)
		}
	}
	if _, err := codecKind(upstream.StreamNode); err == nil {
		t.Fatal("stream accepted as document node")
	}
	for mask := range 1 << len(styles) {
		var wantCodec Style
		var wantParser upstream.Style
		for i, pair := range styles {
			if mask&(1<<i) != 0 {
				wantCodec |= pair.codec
				wantParser |= pair.parser
			}
		}
		gotParser, parserErr := parserStyle(wantCodec)
		gotCodec, codecErr := codecStyle(wantParser)
		if parserErr != nil || codecErr != nil || gotParser != wantParser || gotCodec != wantCodec {
			t.Fatalf("style subset %d: %v, %v, %v, %v", mask, gotParser, gotCodec, parserErr, codecErr)
		}
	}
}
