package calls

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

func TestMetadataAdmissionRejectsExpansionOutsideSelectedDeclarations(t *testing.T) {
	t.Parallel()
	var source strings.Builder
	source.WriteString("on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        a0: &a0 [value]\n")
	for level := 1; level <= 8; level++ {
		fmt.Fprintf(&source, "        a%d: &a%d [*a%d, *a%d, *a%d]\n", level, level, level-1, level-1, level-1)
	}
	source.WriteString("    steps: [{run: echo ok}]\n")
	metadata, err := ParseMetadata([]byte(source.String()))
	if _, ok := errors.AsType[*codec.AdmissionError](err); !ok || metadata != nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("reusable metadata silently dropped rejected job facts: metadata=%v error=%v", metadata, err)
	}
}

func TestMetadataAdmissionPreservesEmptyAndRecursiveErrors(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", " ", "# comment\n"} {
		metadata, err := ParseMetadata([]byte(source))
		if metadata != nil || err == nil || err.Error() != "\"on:\" is not found" {
			t.Fatalf("empty reusable workflow changed: %q: metadata=%v error=%v", source, metadata, err)
		}
	}
	_, err := ParseMetadata([]byte("on: workflow_call\ncycle: &cycle [*cycle]\n"))
	const want = "line:2, column:16: recursive alias \"cycle\" is found. anchor was declared at line:2, column:8"
	if err == nil || err.Error() != want {
		t.Fatalf("recursive metadata rejection changed: got %v, want %s", err, want)
	}
}

// The workflow parser's alias admission decides which callee declarations
// load, so declarations reached through an alias load like literal ones.
func TestMetadataAdmitsAliasedDeclarationsLikeLiteralOnes(t *testing.T) {
	t.Parallel()
	var definitions strings.Builder
	for i := range 650 {
		fmt.Fprintf(&definitions, "  input%d: {type: string}\n", i)
	}
	aliased := "definitions: &definitions\n" + definitions.String() + "on:\n  workflow_call:\n    inputs: *definitions\n"
	literal := "on:\n  workflow_call:\n    inputs:\n" + strings.ReplaceAll(definitions.String(), "  input", "      input")
	for name, source := range map[string]string{"aliased": aliased, "literal": literal} {
		got, err := ParseMetadata([]byte(source))
		if err != nil || len(got.Inputs) != 650 {
			t.Fatalf("%s declarations = %v, error = %v", name, got, err)
		}
	}
}
