package codec

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "github.com/kjanat/go-yaml/v4"
)

func TestUnknownNodeRepresentationRejected(t *testing.T) {
	for _, node := range []*syntax.Node{
		{Kind: 1 << 31},
		{Kind: syntax.ScalarNode, Style: 1 << 31},
		{Kind: syntax.SequenceNode, Content: []*syntax.Node{{Kind: 1 << 31}}},
	} {
		if _, err := Marshal(node); err == nil {
			t.Fatalf("encoded unsupported node: %+v", node)
		}
	}
	for _, node := range []*upstream.Node{
		{Kind: 1 << 31},
		{Kind: upstream.ScalarNode, Style: 1 << 31},
	} {
		if _, err := fromParser(node, nil); err == nil {
			t.Fatalf("accepted unsupported parser node: %+v", node)
		}
	}
}

func TestCursorRejectsNilSyntaxTarget(t *testing.T) {
	root, err := Parse([]byte("key: value\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var node *syntax.Node
	if err := cursor.Decode(node); err == nil {
		t.Fatal("accepted nil syntax target")
	}
}

func TestEncodeSyntaxValuesAndRejectNestedNodes(t *testing.T) {
	node := syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "example"}
	for _, value := range []any{node, &node} {
		encoded, err := Marshal(value)
		if err != nil || string(encoded) != "example\n" {
			t.Fatalf("Marshal = %q, %v", encoded, err)
		}
		converted, err := Encode(value)
		if err != nil || converted.Kind != syntax.ScalarNode || converted.Value != "example" {
			t.Fatalf("Encode = %+v, %v", converted, err)
		}
	}
	for _, value := range []any{
		struct{ Value syntax.Node }{node},
		map[string]any{"value": &node},
		[]any{node},
	} {
		if _, err := Marshal(value); err == nil {
			t.Fatalf("Marshal accepted nested node in %T", value)
		}
		if _, err := Encode(value); err == nil {
			t.Fatalf("Encode accepted nested node in %T", value)
		}
		var output bytes.Buffer
		encoder := NewEncoder(&output)
		if err := encoder.Encode(value); err == nil {
			t.Fatalf("Encoder accepted nested node in %T", value)
		}
		if output.Len() != 0 {
			t.Fatalf("invalid value wrote output: %q", output.String())
		}
		if err := encoder.Encode(node); err != nil {
			t.Fatalf("encoder failed after rejected value: %v", err)
		}
		if err := encoder.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
