package actionlint

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkflowRunOrderedFilters(t *testing.T) {
	for _, tc := range []struct {
		name, producer, config string
		patterns               []string
		want                   string
	}{
		{"excluded", "Build docs", "", []string{"Build*", "!Build docs"}, "exclude every"},
		{"literal expressions", "Build docs", "", []string{"${{ 'Build*' }}", "${{ '!Build docs' }}"}, "exclude every"},
		{"reinclude", "Build docs", "", []string{"Build*", "!Build docs", "Build docs"}, ""},
		{"exclude before include", "Build docs", "", []string{"!Build docs", "Build*"}, ""},
		{"different exclusion", "Build docs", "", []string{"Build*", "!Build CI"}, ""},
		{"escaped leading bang", "!Build docs", "", []string{`\!Build*`, `!\!Build docs`}, "exclude every"},
		{"escaped leading bang restored", "!Build docs", "", []string{`\!Build*`, `!\!Build docs`, `\!Build docs`}, ""},
		{"unknown suffix", "Build docs", "lint: {rules: {correctness: {expression: off}}}", []string{"Build*", "!Build docs", "${{ github.workflow }}"}, ""},
		{"unknown prefix", "Build docs", "lint: {rules: {correctness: {expression: off}}}", []string{"${{ github.workflow }}", "Build*", "!Build docs"}, ""},
		{"rule disabled", "Build docs", "lint: {rules: {correctness: {workflow-run-names: off}}}", []string{"Build*", "!Build docs"}, ""},
		{"invalid suffix", "Build docs", "lint: {rules: {correctness: {glob: off}}}", []string{"Build*", "!Build docs", "Build ["}, ""},
		{"existing missing pattern", "Build docs", "", []string{"Missing*", "Build*", "!Build docs"}, "references workflow pattern"},
		{"unknown producer", "${{ github.workflow }}", "", []string{"Build*", "!Build docs"}, ""},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+strconv.Quote(ending), func(t *testing.T) {
				root := t.TempDir()
				writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: "+strconv.Quote(tc.producer)+"\n"+commandGoodWorkflow)
				config, err := ParseConfig([]byte(tc.config))
				if err != nil {
					t.Fatal(err)
				}
				var source strings.Builder
				source.WriteString("on:\n  workflow_run:\n    workflows:\n")
				for _, pattern := range tc.patterns {
					source.WriteString("      - " + strconv.Quote(pattern) + "\n")
				}
				source.WriteString("    types: [completed]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
				result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(strings.ReplaceAll(source.String(), "\n", ending)), Project: &Project{root: root}, Config: config}}})
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" {
					if len(result.Diagnostics) != 0 {
						t.Fatalf("unexpected findings: %+v", result.Diagnostics)
					}
				} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "workflow-run-names" || !strings.Contains(result.Diagnostics[0].Message, tc.want) || result.Diagnostics[0].Start.Line != 4 || result.Diagnostics[0].Start.Column != 9 {
					t.Fatalf("ordered filters: %+v, want %q at 4:9", result.Diagnostics, tc.want)
				}
			})
		}
	}
}
