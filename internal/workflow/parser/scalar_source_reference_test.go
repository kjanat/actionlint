package parser_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"

	"github.com/google/go-cmp/cmp"
)

type scalarReferenceCase struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

func scalarReferenceCases(t *testing.T) []scalarReferenceCase {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/scalar-source/reference.json")
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
			root, err := codec.Parse([]byte(request.Input))
			if err != nil {
				t.Fatal(err)
			}
			node := root.Content[0].Content[1]
			if want.Kind != "string" || node.Value != want.Value || node.Line != want.Line || node.Column != want.Col {
				t.Fatalf("reference scalar = %+v; local = %q at %d:%d", want, node.Value, node.Line, node.Column)
			}
			p := workflowparser.NewSourceParser([]byte(request.Input))
			if p.ScriptSource(node) == nil {
				t.Fatal("reference-compatible scalar has no source mapping")
			}
		})
	}
}

// Live verification is opt-in; ordinary tests use the checked-in reference results.
func TestScalarSourceReferenceLive(t *testing.T) {
	image := os.Getenv("ACTIONLINT_REFERENCE_IMAGE")
	if image == "" {
		t.Skip("set ACTIONLINT_REFERENCE_IMAGE to a locally available Runny Docker image")
	}
	cases := scalarReferenceCases(t)
	for _, fixture := range cases {
		var request struct {
			ID, Operation, Engine, Input string
			AllowAnchors                 bool
		}
		if err := json.Unmarshal(fixture.Request, &request); err != nil {
			t.Fatal(err)
		}
		t.Run(request.ID, func(t *testing.T) {
			if request.Operation != "parse-yaml" || request.Engine != "next" || !request.AllowAnchors {
				t.Fatal("reference case requires an unsupported Docker invocation")
			}
			var envelope struct {
				ID              string
				ProtocolVersion int
			}
			if err := json.Unmarshal(fixture.Response, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.ID != request.ID || envelope.ProtocolVersion != 1 {
				t.Fatal("recorded response envelope does not match its request")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--pull=never", image, request.Operation, "--engine", request.Engine, "--anchors")
			command.Stdin = bytes.NewBufferString(request.Input)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil {
				t.Fatalf("reference failed: %v\n%s", err, stderr.String())
			}
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
			recorded := normalize(fixture.Response)
			// These two fields belong to the recorded transport envelope, not Runny's report.
			delete(recorded, "id")
			delete(recorded, "protocolVersion")
			if diff := cmp.Diff(recorded, normalize(output)); diff != "" {
				t.Errorf("reference response changed (-recorded +live):\n%s", diff)
			}
		})
	}
	t.Logf("verified %d live reference responses", len(cases))
}
