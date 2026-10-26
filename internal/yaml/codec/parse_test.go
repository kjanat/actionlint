package codec

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestParseEmptyInput(t *testing.T) {
	node, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if node == nil || node.Kind != 0 {
		t.Fatalf("Parse(empty) = %#v, want non-nil zero-kind node", node)
	}
}

func TestParsePreservesAliasTargetIdentityAndFirstDocument(t *testing.T) {
	node, err := Parse([]byte("first: &value source\ncopy: *value\n---\nsecond: ignored\n"))
	if err != nil {
		t.Fatal(err)
	}
	if node == nil || node.Kind != syntax.DocumentNode || len(node.Content) != 1 {
		t.Fatalf("parsed document = %#v", node)
	}
	mapping := node.Content[0]
	if mapping.Kind != syntax.MappingNode || len(mapping.Content) != 4 {
		t.Fatalf("first document mapping = %#v", mapping)
	}
	anchorValue := mapping.Content[1]
	alias := mapping.Content[3]
	if alias.Kind != syntax.AliasNode || alias.Alias != anchorValue {
		t.Fatalf("alias target %p, want original anchor node %p", alias.Alias, anchorValue)
	}
	if mapping.Content[0].Value != "first" || mapping.Content[2].Value != "copy" {
		t.Fatalf("unexpected first document keys: %q, %q", mapping.Content[0].Value, mapping.Content[2].Value)
	}
}

func TestParseMalformedInput(t *testing.T) {
	node, err := Parse([]byte("key: [\n"))
	if err == nil || node != nil {
		t.Fatalf("Parse(malformed) = %#v, %v; want nil node and error", node, err)
	}
}
