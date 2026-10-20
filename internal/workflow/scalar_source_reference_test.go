package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"

	"github.com/google/go-cmp/cmp"
)

type scalarReferenceCase struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

func scalarReferenceCases(t *testing.T) []scalarReferenceCase {
	t.Helper()
	data, err := os.ReadFile("../../testdata/conformance/scalar-source/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []scalarReferenceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("empty reference corpus")
	}
	return fixture.Cases
}

func TestScalarSourceReferenceRecorded(t *testing.T) {
	for _, fixture := range scalarReferenceCases(t) {
		var request struct{ ID, Input string }
		if err := json.Unmarshal(fixture.Request, &request); err != nil {
			t.Fatal(err)
		}
		t.Run(request.ID, func(t *testing.T) {
			var response struct {
				RunnerSHA string
				Engines   []struct {
					Parsed bool
					YAML   struct {
						Items []struct {
							Value struct {
								Kind, Value string
								Line, Col   int
							}
						}
					}
				}
			}
			if err := json.Unmarshal(fixture.Response, &response); err != nil {
				t.Fatal(err)
			}
			if response.RunnerSHA == "" || len(response.Engines) != 1 || !response.Engines[0].Parsed || len(response.Engines[0].YAML.Items) != 1 {
				t.Fatal("reference did not return the expected parsed YAML mapping")
			}
			want := response.Engines[0].YAML.Items[0].Value
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(request.Input), &root); err != nil {
				t.Fatal(err)
			}
			node := root.Content[0].Content[1]
			if want.Kind != "string" || node.Value != want.Value || node.Line != want.Line || node.Column != want.Col {
				t.Fatalf("reference scalar = %+v; local = %q at %d:%d", want, node.Value, node.Line, node.Column)
			}
			p := workflowlang.NewSourceParser([]byte(request.Input))
			if p.ScriptSource(node) == nil {
				t.Fatal("reference-compatible scalar has no source mapping")
			}
		})
	}
}

// Live verification is opt-in; ordinary tests use the checked-in reference results.
func TestScalarSourceReferenceLive(t *testing.T) {
	oracle, wasm := os.Getenv("ACTIONLINT_REFERENCE_ORACLE"), os.Getenv("ACTIONLINT_REFERENCE_WASM")
	if oracle == "" && wasm == "" {
		t.Skip("set ACTIONLINT_REFERENCE_ORACLE and ACTIONLINT_REFERENCE_WASM")
	}
	if oracle == "" || wasm == "" {
		t.Fatal("both reference paths are required")
	}
	cases := scalarReferenceCases(t)
	var input bytes.Buffer
	for _, fixture := range cases {
		if err := json.Compact(&input, fixture.Request); err != nil {
			t.Fatal(err)
		}
		input.WriteByte('\n')
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, oracle, "--timeout", "55s", "--wasm", wasm)
	command.Stdin = &input
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("reference failed: %v\n%s", err, stderr.String())
	}
	lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
	if len(lines) != len(cases) {
		t.Fatalf("expected %d responses, got %d", len(cases), len(lines))
	}
	for i, fixture := range cases {
		normalize := func(data []byte) map[string]any {
			t.Helper()
			var result map[string]any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if engines, ok := result["engines"].([]any); ok {
				for _, engine := range engines {
					if fields, ok := engine.(map[string]any); ok {
						delete(fields, "ns")
						delete(fields, "alloc")
					}
				}
			}
			return result
		}
		if diff := cmp.Diff(normalize(fixture.Response), normalize(lines[i])); diff != "" {
			t.Errorf("reference response %d changed (-recorded +live):\n%s", i, diff)
		}
	}
	t.Logf("verified %d live reference responses", len(cases))
}
