package results_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/render"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCheckResultSchema(t *testing.T) {
	data, err := os.ReadFile("schemas/results/v1.schema.json")
	if err != nil {
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
		analysis, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "action.yml", Content: []byte(`name: Test
description: Test action
runs:
  ` + runtime + "\n")}})
		if err != nil {
			t.Fatal(err)
		}
		result := render.NewCheckResult(0)
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
		data, err := json.Marshal(render.NewCheckResult(0))
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
	diagnostic := diagmodel.Diagnostic{Rule: "shellcheck", Code: "SC2086", Severity: "warning", Message: "Quote variable", Path: "ci.yml",
		Start: diagmodel.Position{Line: 2, Column: 3}, End: diagmodel.Position{Line: 3, Column: 4}, Snippet: "echo $value",
		Fixes: []diagmodel.Fix{{Description: "Quote", Edits: []diagmodel.Edit{{Path: "ci.yml", Start: diagmodel.Position{Line: 2, Column: 3}, End: diagmodel.Position{Line: 3, Column: 4}, Replacement: "\"$value\""}}}}}
	for _, code := range []int{0, 1, 2, 3, 99} {
		result := render.NewCheckResult(code)
		analysis, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "ci.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
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
		if err := result.WriteJSON(&output, false); err != nil {
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
	if err := compiler.AddResource("https://example.com/diagnostic.schema.json", document); err != nil {
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
	lineSchema, err := compiler.Compile("https://example.com/diagnostic.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	result := render.NewCheckResult(1)
	result.Diagnostics = []diagmodel.Diagnostic{diagnostic, diagnostic}
	var lines bytes.Buffer
	if err := result.WriteJSON(&lines, true); err != nil {
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
