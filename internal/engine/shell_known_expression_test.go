package engine

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

func TestKnownShellExpressions(t *testing.T) {
	tests := []struct {
		name     string
		runner   string
		defaults string
		step     string
		message  string
		line     int
		column   int
	}{
		{"runner object", `${{ fromJSON('{"labels":["windows-latest"]}') }}`, "", "sh", `shell name "sh" is invalid on Windows`, 7, 16},
		{"runner object casing", `${{ fromJSON('{"LaBeLs":"Windows-latest"}') }}`, "", "sh", `shell name "sh" is invalid on Windows`, 7, 16},
		{"runner scalar", `${{ 'windows-latest' }}`, "", "sh", `shell name "sh" is invalid on Windows`, 7, 16},
		{"runner labels expression", `{labels: "${{ fromJSON('[\"windows-latest\"]') }}"}`, "", "sh", `shell name "sh" is invalid on Windows`, 7, 16},
		{"runner unknown", `${{ vars.RUNNER }}`, "", "sh", "", 0, 0},
		{"default shell", "ubuntu-latest", `${{ fromJSON('{"shell":"zsh"}') }}`, "", `shell name "zsh" is invalid`, 6, 12},
		{"default shell casing", "windows-latest", `${{ fromJSON('{"ShElL":"sh"}') }}`, "", `shell name "sh" is invalid on Windows`, 6, 12},
		{"default converted scalar", "ubuntu-latest", `${{ fromJSON('{"shell":42}') }}`, "", `shell name "42" is invalid`, 6, 12},
		{"default expression text stays data", "ubuntu-latest", `${{ fromJSON('{"shell":"${{ secrets.SHELL }}"}') }}`, "", `shell name "${{ secrets.SHELL }}" is invalid`, 6, 12},
		{"default unknown", "ubuntu-latest", `${{ fromJSON(vars.DEFAULTS) }}`, "", "", 0, 0},
		{"default invalid shape owned by expression", "ubuntu-latest", `${{ fromJSON('{"shell":[]}') }}`, "", "", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ` + tc.runner + "\n"
			if tc.defaults != "" {
				source += "    defaults:\n      run: " + tc.defaults + "\n"
			}
			source += `    steps:
      - run: echo test
`
			if tc.step != "" {
				source += "        shell: " + tc.step + "\n"
			}
			request := Request{}
			var err error

			request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errors, err := testFindings(runTestAnalysis(t.Context(), request))
			if err != nil {
				t.Fatal(err)
			}
			var found []*findingmodel.Finding
			for _, diagnostic := range errors {
				if diagnostic.Rule == "shell-name" {
					found = append(found, diagnostic)
				}
			}
			if tc.message == "" {
				if len(found) != 0 {
					t.Fatalf("unexpected shell diagnostics: %v", found)
				}
				if tc.name == "default invalid shape owned by expression" && (len(errors) != 1 || errors[0].Rule != "expression") {
					t.Fatalf("expected exactly one expression diagnostic, got %v", errors)
				}
				return
			}
			if len(found) != 1 || !strings.Contains(found[0].Message, tc.message) {
				t.Fatalf("expected one %q diagnostic, got %v", tc.message, errors)
			}
			if found[0].Start.Line != tc.line || found[0].Start.Column != tc.column {
				t.Fatalf("diagnostic position %d:%d, want %d:%d", found[0].Start.Line, found[0].Start.Column, tc.line, tc.column)
			}
		})
	}
}

func TestKnownShellCommandHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index < 0 || index >= len(os.Args) {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	switch os.Args[index] {
	case "shellcheck":
		fmt.Fprintln(os.Stdout, `{"comments":[{"line":2,"endLine":2,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"shellcheck invoked"}]}`)
	default:
		t.Fatalf("unexpected helper %q", os.Args[index])
	}
	os.Exit(0)
}

func TestKnownShellStateReset(t *testing.T) {
	workflow, errors := Parse([]byte(`on: push
jobs:
  python:
    runs-on: ${{ fromJSON('{"labels":"windows-latest"}') }}
    defaults:
      run: ${{ fromJSON('{"shell":"python"}') }}
    steps:
      - run: print("hello")
  bash:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	collector := &compositeAnalyzer{collectScripts: true, resolveScriptExecution: true, actions: actionlocal.NewResolver(nil, nil)}
	collector.beginWorkflow(workflow)
	for _, id := range []string{"python", "bash"} {
		job := workflow.Jobs[id]
		collector.beginJob(job)
		collector.collectScript(job.Steps[0], nil, "", nil, nil, nil)
		if got := collector.scripts[len(collector.scripts)-1].execution.shell.Name; got != id {
			t.Fatalf("job %s resolved shell %q", id, got)
		}
	}
	job := workflow.Jobs["python"]
	if job.Defaults.Run.Shell != nil || len(job.RunsOn.Labels) != 0 {
		t.Fatal("shell resolution mutated the source AST")
	}
}
