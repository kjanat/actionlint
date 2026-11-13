package results_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	render "actionlint.kjanat.dev/v2/internal/output"

	diagmodel "actionlint.kjanat.dev/v2/internal/results"

	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/results/report"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCheckResultSchema(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("schemas/results/v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	partial := report.Analysis{IncompleteChecks: []report.IncompleteCheck{{Source: sourcepos.Reference{Document: "/work/ci.yml", Revision: "abc"}, Rule: "workflow-run-names", Reason: "producer names unavailable"}}}
	entry := sourcepos.Reference{Document: "/work/ci.yml", Revision: "abc", Node: "root"}
	partial.IncompleteChecks = append(partial.IncompleteChecks, report.IncompleteCheck{
		Source:     sourcepos.Reference{Document: "/work/action.yml", Revision: "def"},
		Invocation: report.CheckInvocation{Entry: entry, ID: invocation.ID(strings.Repeat("a", 64))},
		Rule:       "shellcheck", Reason: "tool unavailable",
	})
	encoded, err := json.Marshal(commandstatus.AnalysisResult(partial))
	if err != nil {
		t.Fatal(err)
	}
	var projected map[string]any
	if err := json.Unmarshal(encoded, &projected); err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	const location = "https://example.com/result.schema.json"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(projected); err != nil {
		t.Fatalf("incomplete-check result violates its schema: %v\n%s", err, encoded)
	}
	var records struct {
		Checks []map[string]json.RawMessage `json:"incomplete_checks"`
	}
	if err := json.Unmarshal(encoded, &records); err != nil {
		t.Fatal(err)
	}
	if len(records.Checks) != 2 || records.Checks[0]["invocation"] != nil || records.Checks[1]["invocation"] == nil {
		t.Fatalf("unknown invocation was invented or known invocation lost: %s", encoded)
	}
	for _, invalid := range []string{
		`null`, `{}`, `{"id":"` + strings.Repeat("a", 64) + `"}`,
		`{"entry":{"document":"ci.yml","revision":"abc","node":"root","address":""}}`,
		`{"entry":{"document":"ci.yml","revision":"abc","node":"root","address":""},"id":"bad"}`,
		`{"entry":{"document":"ci.yml","revision":"","node":"root","address":""},"id":"` + strings.Repeat("a", 64) + `"}`,
	} {
		var identity any
		if err := json.Unmarshal([]byte(invalid), &identity); err != nil {
			t.Fatal(err)
		}
		projected["incomplete_checks"] = []any{map[string]any{
			"source": map[string]any{"document": "action.yml", "revision": "def", "node": "", "address": ""},
			"reason": "tool unavailable", "invocation": identity,
		}}
		if err := schema.Validate(projected); err == nil {
			t.Errorf("schema accepted malformed invocation: %s", invalid)
		}
	}
	for _, runtime := range []string{
		`using: composite
  steps:
    - uses: actions/checkout@v7`,
		`using: node24
  main: action.mjs
  pre: pre.mjs`,
		`using: docker
  image: Dockerfile
  args: [hello]`,
		"plugin: internal/check",
		"using: future-runtime",
	} {
		analysis, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "action.yml", Content: []byte(`name: Test
description: Test action
runs:
  ` + runtime + "\n")}})
		if err != nil {
			t.Fatal(err)
		}
		result := commandstatus.NewCheckResult(0)
		result.Documents = analysis.Documents
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("runtime %q: %v\n%s", runtime, err, data)
		}
	}
	for _, invalid := range []string{
		`{"kind":"workflow","path":"ci.yml","parse_status":"complete","triggers":[],"jobs":[],"runs":{"kind":"composite","steps":[]}}`,
		`{"kind":"action","path":"action.yml","parse_status":"complete","inputs":[],"outputs":[],"jobs":[],"runs":{"kind":"composite","steps":[]}}`,
		`{"kind":"action","path":"action.yml","parse_status":"complete","inputs":[],"outputs":[],"runs":{"kind":"javascript","using":"node24","steps":[]}}`,
	} {
		data, err := json.Marshal(commandstatus.NewCheckResult(0))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal([]byte(invalid), &document); err != nil {
			t.Fatal(err)
		}
		value["documents"] = []any{document}
		if err := schema.Validate(value); err == nil {
			t.Fatalf("schema accepted mixed document/runtime: %s", invalid)
		}
	}
	diagnostic := diagmodel.Diagnostic{
		Rule: "shellcheck", Code: "SC2086", Severity: "warning", Message: "Quote variable", Path: "ci.yml",
		Start: sourcepos.Position{Line: 2, Column: 3}, End: sourcepos.Position{Line: 3, Column: 4}, Snippet: "echo $value",
		Fixes: []diagmodel.Fix{{Description: "Quote", Edits: []diagmodel.Edit{{Path: "ci.yml", Start: sourcepos.Position{Line: 2, Column: 3}, End: sourcepos.Position{Line: 3, Column: 4}, Replacement: "\"$value\""}}}},
	}
	for _, code := range []int{0, 1, 2, 3, 99} {
		result := commandstatus.NewCheckResult(code)
		analysis, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "ci.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`)}})
		if err != nil {
			t.Fatal(err)
		}
		result.Documents = analysis.Documents
		if code == 1 || code == 3 {
			result.Diagnostics = []diagmodel.Diagnostic{diagnostic}
		}
		if code < 2 {
			count := 1
			result.FileCount = &count
		} else {
			result.Error = "could not complete"
		}
		var output bytes.Buffer
		if err := render.WriteJSON(&output, result, false); err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(output.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		// Future additive metadata is accepted without losing the status constraints.
		value["future"] = map[string]any{"jobs": []string{"test"}}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("exit %d: %v\n%s", code, err, &output)
		}
		value["completed"] = !result.Completed
		if err := schema.Validate(value); err == nil {
			t.Fatalf("exit %d: accepted contradictory completion status", code)
		}
	}
	data, err = os.ReadFile("schemas/results/v1.diagnostic.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("https://example.com/results.schema.json", document); err != nil {
		t.Fatal(err)
	}
	// Resolve the relative reference to the exact result schema under test.
	data, err = os.ReadFile("schemas/results/v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("https://example.com/v1.schema.json", document); err != nil {
		t.Fatal(err)
	}
	lineSchema, err := compiler.Compile("https://example.com/results.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	result := commandstatus.NewCheckResult(1)
	result.Diagnostics = []diagmodel.Diagnostic{diagnostic, diagnostic}
	var lines bytes.Buffer
	if err := render.WriteJSON(&lines, result, true); err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(lines.String()), "\n") {
		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		if err := lineSchema.Validate(value); err != nil {
			t.Fatal(err)
		}
		var record struct {
			SchemaVersion int `json:"schema_version"`
			diagmodel.Diagnostic
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.SchemaVersion != 1 || record.End.Line != 3 || len(record.Fixes) != 1 || record.Code != "SC2086" {
			t.Fatalf("JSONL lost canonical metadata: %s", line)
		}
	}
}
