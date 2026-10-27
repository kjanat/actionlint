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
	var plain struct {
		On struct {
			Call struct {
				Inputs map[string]struct {
					Type string `yaml:"type"`
				} `yaml:"inputs"`
			} `yaml:"workflow_call"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(source), &plain); err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("plain metadata error = %v, want alias limit", err)
	}
	if _, err := ParseMetadata([]byte(source)); err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("metadata error = %v, want selected alias expansion limit", err)
	}

	literal := "on:\n  workflow_call:\n    inputs:\n" + strings.ReplaceAll(definitions.String(), "  input", "      input")
	got, err := ParseMetadata([]byte(literal))
	if err != nil || len(got.Inputs) != 650 {
		t.Fatalf("literal declarations = %v, error = %v", got, err)
	}
}
