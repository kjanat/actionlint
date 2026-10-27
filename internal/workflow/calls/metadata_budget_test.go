package calls

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"github.com/kjanat/go-yaml/v4"
)

func TestMetadataAdmissionRejectsExpansionOutsideSelectedDeclarations(t *testing.T) {
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

func TestMetadataDecoderKeepsSelectedAliasBudget(t *testing.T) {
	var definitions strings.Builder
	for i := range 650 {
		fmt.Fprintf(&definitions, "  input%d: {type: string}\n", i)
	}
	source := "definitions: &definitions\n" + definitions.String() + "on:\n  workflow_call:\n    inputs: *definitions\n"
	type target struct {
		On struct {
			Call struct {
				Inputs map[string]struct {
					Type string `yaml:"type"`
				} `yaml:"inputs"`
			} `yaml:"workflow_call"`
		} `yaml:"on"`
	}
	var plain target
	if err := yaml.Unmarshal([]byte(source), &plain); err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("plain metadata error = %v, want alias limit", err)
	}
	if _, err := ParseMetadata([]byte(source)); err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("metadata error = %v, want selected alias expansion limit", err)
	}
	var trailingSource strings.Builder
	trailingSource.WriteString(source)
	for i := range 19 {
		fmt.Fprintf(&trailingSource, "trailing%d: ignored\n", i)
	}
	withTrailingKeys := trailingSource.String()
	plain = target{}
	wantErr := yaml.Unmarshal([]byte(withTrailingKeys), &plain)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream trailing-key error = %v, want alias limit", wantErr)
	}
	if _, err := ParseMetadata([]byte(withTrailingKeys)); err == nil || err.Error() != wantErr.Error() {
		t.Fatalf("metadata trailing-key error = %v, want exact upstream failure %v", err, wantErr)
	}

	literal := "on:\n  workflow_call:\n    inputs:\n" + strings.ReplaceAll(definitions.String(), "  input", "      input")
	got, err := ParseMetadata([]byte(literal))
	if err != nil || len(got.Inputs) != 650 {
		t.Fatalf("literal declarations = %v, error = %v", got, err)
	}
}
