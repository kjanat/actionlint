package workflow

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestParseFlowMappingEmptyValueCompatibility(t *testing.T) {
	const jobs = "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	for _, tc := range []struct {
		name   string
		source string
		line   int
		column int
	}{
		{"reported workflow", "name: autofix.ci\nrun-name: test\non: { pull_request:, push: { branches: [master] } }", 3, 7},
		{"multiline flow", "on: {\n  pull_request:,\n  push: null\n}", 2, 3},
		{"unicode prefix", "name: café\non: { pull_request:, push: null }", 2, 7},
		{"BOM and CRLF", "\ufeffname: test\r\non: { pull_request:, push: null }", 2, 7},
		{"closing brace", "on: { pull_request:}", 1, 7},
		{"space before colon", "on: { pull_request :, push: null }", 1, 7},
		{"unicode same line", "env: {é: null, missing:, ok: yes}\non: push", 1, 16},
		{"shorthand mapping", "on: { pull_request, push }", 0, 0},
		{"explicit key with comment", "on: { ? pull_request # end key\n:, push: null }", 0, 0},
		{"space after colon", "on: { pull_request: , push: null }", 0, 0},
		{"tab after colon", "on: { pull_request:\t, push: null }", 0, 0},
		{"newline after colon", "on: { pull_request:\n, push: null }", 0, 0},
		{"explicit null", "on: { pull_request: null, push: ~ }", 0, 0},
		{"block mapping", "on:\n  pull_request:\n  push:", 0, 0},
		{"single quoted key", "on: { 'pull_request':, push: null }", 0, 0},
		{"double quoted key", "on: { \"pull_request\":, push: null }", 0, 0},
		{"quoted scalar", "name: 'pull_request:,'\non: push", 0, 0},
		{"block scalar", "name: |\n  { pull_request:, push: null }\non: push", 0, 0},
		{"comment", "on: push # { pull_request:, push: null }", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, findings := Parse([]byte(tc.source + jobs))
			if workflow == nil || len(workflow.Jobs) != 1 {
				t.Fatalf("workflow recovery failed: %v", findings)
			}
			if tc.line == 0 {
				if len(findings) != 0 {
					t.Fatalf("valid workflow rejected: %v", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("want one compatibility diagnostic, got %v", findings)
			}
			finding := findings[0]
			if finding.Kind != "syntax-check" || finding.Line != tc.line || finding.Column != tc.column ||
				!strings.Contains(finding.Message, "GitHub Actions") || !strings.Contains(finding.Message, "space") {
				t.Fatalf("unexpected diagnostic: %v", finding)
			}
		})
	}
}

func TestFlowMappingNullsPhysicalTree(t *testing.T) {
	for _, tc := range []struct {
		source string
		count  int
	}{
		{"[missing:]", 1},
		{"[missing:, next]", 1},
		{"[missing: ]", 0},
		{"[\"missing\":]", 0},
		{"{first:, second:, third:}", 3},
		{"decl: &shared {missing:, ok: null}\nuse: [*shared, *shared]", 1},
		{"{? :, ok: null}", 0},
		{"{? missing\n:, ok: null}", 1},
		{"{? missing # comment\n:, ok: null}", 0},
		{"{missing: # comment\n, ok: null}", 0},
		{"{missing: &empty , ok: *empty}", 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.source), &root); err != nil {
				t.Fatal(err)
			}
			parser := NewSourceParser([]byte(tc.source))
			parser.IndexScalarSources(&root)
			parser.checkFlowMappingNulls(&root, false)
			if len(parser.Errors) != tc.count {
				t.Fatalf("want %d diagnostics, got %v", tc.count, parser.Errors)
			}
		})
	}
}
