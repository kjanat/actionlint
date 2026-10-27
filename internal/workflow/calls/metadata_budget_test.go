package calls

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

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
