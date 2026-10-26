package codec_test

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type unsafeNodeResult struct{ result any }

func (m unsafeNodeResult) MarshalYAML() (any, error) { return m.result, nil }

func TestMarshalerCannotReturnSyntaxNodes(t *testing.T) {
	node := syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "hidden"}
	for _, tc := range []struct {
		name   string
		result any
	}{
		{name: "node value", result: node},
		{name: "node pointer", result: &node},
		{name: "node in returned slice", result: []any{node}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := unsafeNodeResult{result: tc.result}
			if _, err := codec.Marshal(value); err == nil {
				t.Fatal("Marshal accepted a syntax node returned by a Marshaler")
			}
			if _, err := codec.Encode(value); err == nil {
				t.Fatal("Encode accepted a syntax node returned by a Marshaler")
			}
		})
	}
}

type safeYAMLMarshaler struct {
	Node   *syntax.Node
	Parent *safeYAMLMarshaler
}

func (safeYAMLMarshaler) MarshalYAML() (any, error) {
	return map[string]string{"safe": "yaml"}, nil
}

type safeTextMarshaler struct {
	Node   *syntax.Node
	Parent *safeTextMarshaler
}

func (safeTextMarshaler) MarshalText() ([]byte, error) { return []byte("safe text"), nil }

func TestSafeMarshalersHideSyntaxNodesAndParentCycles(t *testing.T) {
	node := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "internal"}
	yamlValue := &safeYAMLMarshaler{Node: node}
	yamlValue.Parent = yamlValue
	textValue := &safeTextMarshaler{Node: node}
	textValue.Parent = textValue
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "YAML Marshaler", value: yamlValue, want: "safe: yaml\n"},
		{name: "TextMarshaler", value: textValue, want: "safe text\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := codec.Marshal(tc.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("Marshal = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNilSyntaxNodeFieldEncodesAsNull(t *testing.T) {
	value := struct {
		Node *syntax.Node `yaml:"node"`
	}{}
	encoded, err := codec.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal nil node field: %v", err)
	}
	if string(encoded) != "node: null\n" {
		t.Fatalf("Marshal nil node field = %q, want %q", encoded, "node: null\n")
	}
	wantBytes, err := upstream.Marshal(value)
	if err != nil {
		t.Fatalf("upstream.Marshal nil node field baseline: %v", err)
	}
	if !bytes.Equal(encoded, wantBytes) {
		t.Fatalf("codec Marshal = %q, upstream baseline = %q", encoded, wantBytes)
	}
	decoded, err := codec.Encode(value)
	if err != nil {
		t.Fatalf("Encode nil node field: %v", err)
	}
	if decoded.Kind != syntax.MappingNode || len(decoded.Content) != 2 {
		t.Fatalf("Encode nil node field = %#v, want one mapping property", decoded)
	}
	var baseline upstream.Node
	if err := baseline.Encode(value); err != nil {
		t.Fatalf("upstream.Node.Encode nil node field baseline: %v", err)
	}
	null := decoded.Content[1]
	wantNull := baseline.Content[1]
	if null.Kind != syntax.ScalarNode || null.Tag != wantNull.Tag || null.Value != wantNull.Value || null.Tag != "!!null" {
		t.Fatalf("encoded field = %#v, upstream baseline = %#v; want YAML null", null, wantNull)
	}
}

func TestEncoderWritesNothingOnMarshalerFailureAndRemainsUsable(t *testing.T) {
	node := syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "unsafe"}
	var output bytes.Buffer
	encoder := codec.NewEncoder(&output)
	if err := encoder.Encode(unsafeNodeResult{result: node}); err == nil {
		t.Fatal("Encoder accepted syntax node returned by Marshaler")
	}
	if output.Len() != 0 {
		t.Fatalf("failed Encode wrote %q", output.String())
	}
	if err := encoder.Encode("after failure"); err != nil {
		t.Fatalf("Encoder unusable after failed value: %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("Encoder.Close: %v", err)
	}
	if output.String() != "after failure\n" {
		t.Fatalf("Encoder output = %q, want %q", output.String(), "after failure\n")
	}
}

func TestEncodeReturnsAValueNodeWithZeroCoordinates(t *testing.T) {
	value := map[string]any{"nested": []any{"value", map[string]string{"leaf": "text"}}}
	encoded, err := codec.Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded.Kind != syntax.MappingNode {
		t.Fatalf("Encode root kind = %v, want MappingNode value", encoded.Kind)
	}
	seen := map[*syntax.Node]bool{}
	var visit func(*syntax.Node)
	visit = func(node *syntax.Node) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		if node.Line != 0 || node.Column != 0 {
			t.Errorf("Encode assigned source coordinates to node %#v", node)
		}
		visit(node.Alias)
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(encoded)
	if len(seen) == 0 {
		t.Fatal("Encode returned an empty graph")
	}
}
