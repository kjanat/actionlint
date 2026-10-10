package actionlint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCheckResultRetainsCachedConfiguration(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "actionlint.yaml")
	workflow := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(config, []byte("config-variables: [MY_VAR]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`), 0600); err != nil {
		t.Fatal(err)
	}
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: dir, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		analysis, err := session.Files([]string{workflow}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := analysis.CheckResult()
		if len(result.Configs) != 1 || result.Configs[0].File != config || len(result.Configs[0].Origins) == 0 {
			t.Fatalf("configuration provenance lost: %+v", result.Configs)
		}
	}
}

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
		action, _ := ParseActionOutline("action.yml", []byte(`name: Test
description: Test action
runs:
  `+runtime+"\n"))
		result := NewCheckResult(0)
		result.Documents = DocumentOutlines{action}
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
		data, err := json.Marshal(NewCheckResult(0))
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
	diagnostic := Diagnostic{Rule: "shellcheck", Code: "SC2086", Severity: "warning", Message: "Quote variable", Path: "ci.yml",
		Start: DiagnosticPosition{2, 3}, End: DiagnosticPosition{3, 4}, Snippet: "echo $value",
		Fixes: []DiagnosticFix{{Description: "Quote", Edits: []DiagnosticEdit{{Path: "ci.yml", Start: DiagnosticPosition{2, 3}, End: DiagnosticPosition{3, 4}, Replacement: "\"$value\""}}}}}
	for _, code := range []int{0, 1, 2, 3, 99} {
		result := NewCheckResult(code)
		workflow, parseErrors := Parse([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`))
		if len(parseErrors) != 0 {
			t.Fatal(parseErrors)
		}
		result.Documents = DocumentOutlines{workflowOutline("ci.yml", workflow, false)}
		if code == 1 || code == 3 {
			result.Diagnostics = []Diagnostic{diagnostic}
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
	result := NewCheckResult(1)
	result.Diagnostics = []Diagnostic{diagnostic, diagnostic}
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
			Diagnostic
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.SchemaVersion != 1 || record.End.Line != 3 || len(record.Fixes) != 1 || record.Code != "SC2086" {
			t.Fatalf("JSONL lost canonical metadata: %s", line)
		}
	}
}
