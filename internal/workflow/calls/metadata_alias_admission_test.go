package calls

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestMetadataAliasAdmissionRejectsUndeclaredSource(t *testing.T) {
	root, err := codec.Parse([]byte("anchor: &a value\nalias: *a\n"))
	if err != nil {
		t.Fatal(err)
	}
	mapping := root.Content[0]
	alias := mapping.Content[3]
	mapping.Content = []*syntax.Node{mapping.Content[2], alias, mapping.Content[0], mapping.Content[1]}
	err = checkMetadataAliases(root)
	want := fmt.Sprintf("line:%d, column:%d: alias %q refers to an anchor not yet declared", alias.Line, alias.Column, "a")
	if err == nil || err.Error() != want {
		t.Fatalf("metadata alias admission=%v, want %q", err, want)
	}
	if mapping.Content[1] != alias {
		t.Fatal("metadata alias admission changed rejected source")
	}
}

func TestMetadataAliasesCanReferenceOtherRootSections(t *testing.T) {
	metadata, err := ParseMetadata([]byte("definitions: &definitions {flag: {type: boolean}}\non:\n  workflow_call:\n    inputs: *definitions\n"))
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil || len(metadata.Inputs) != 1 {
		t.Fatalf("root-section alias lost input declarations: %+v", metadata)
	}
	if _, exists := metadata.Inputs["flag"]; !exists {
		t.Fatal("root-section alias lost flag input")
	}
}
