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
			linter, err := NewLinter(io.Discard, &LinterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			errors, err := linter.Lint("test.yaml", []byte(source), nil)
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

func TestKnownShellExternalLinters(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		runner        string
		workflowShell string
		defaults      string
		stepShell     string
		want          string
	}{
		{"Windows object skips ShellCheck", `${{ fromJSON('{"labels":"windows-latest"}') }}`, "", "", "", ""},
		{"Windows labels expression skips ShellCheck", `{labels: "${{ fromJSON('[\"windows-latest\"]') }}"}`, "", "", "", ""},
		{"Windows explicit override", `${{ fromJSON('{"labels":"windows-latest"}') }}`, "", "", "bash", "shellcheck"},
		{"known bash default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"bash"}') }}`, "", "shellcheck"},
		{"known Python default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python"}') }}`, "", ""},
		{"known custom Python default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python -u {0}"}') }}`, "", ""},
		{"known default casing", "ubuntu-latest", "", `${{ fromJSON('{"ShElL":"python"}') }}`, "", ""},
		{"known PowerShell default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"pwsh"}') }}`, "", ""},
		{"known default overrides workflow", "ubuntu-latest", "python", `${{ fromJSON('{"shell":"bash"}') }}`, "", "shellcheck"},
		{"step overrides known default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python"}') }}`, "bash", "shellcheck"},
		{"dynamic custom Bash retains dispatch", "ubuntu-latest", "", "", `bash ${{ vars.FLAGS }} {0}`, "shellcheck"},
		{"missing shell inherits workflow", "ubuntu-latest", "python", `${{ fromJSON('{"working-directory":"src"}') }}`, "", ""},
		{"empty mapping inherits workflow", "ubuntu-latest", "python", `${{ fromJSON('{}') }}`, "", ""},
		{"unknown map masks workflow Python", "ubuntu-latest", "python", `${{ fromJSON(vars.DEFAULTS) }}`, "", ""},
		{"unknown map masks workflow Bash", "ubuntu-latest", "bash", `${{ fromJSON(vars.DEFAULTS) }}`, "", ""},
		{"invalid map masks workflow", "ubuntu-latest", "python", `${{ fromJSON('{"shell":[]}') }}`, "", ""},
		{"evaluated delimiters stay data", "ubuntu-latest", "python", `${{ fromJSON('{"shell":"${{ secrets.SHELL }}"}') }}`, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\n"
			if tc.workflowShell != "" {
				source += `defaults:
  run:
    shell: ` + tc.workflowShell + "\n"
			}
			source += `jobs:
  test:
    runs-on: ` + tc.runner + "\n"
			if tc.defaults != "" {
				source += "    defaults:\n      run: " + tc.defaults + "\n"
			}
			source += `    steps:
      - run: echo test
`
			if tc.stepShell != "" {
				source += "        shell: " + tc.stepShell + "\n"
			}
			session, err := NewAnalysisSession(AnalysisOptions{ShellcheckOptions: &ExternalCommandOptions{Executable: &exe, Arguments: []string{"-test.run=^TestKnownShellCommandHelper$", "--", "shellcheck"}}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.content("test.yaml", []byte(source), nil, false)
			if err != nil {
				t.Fatal(err)
			}
			errors := result.legacyErrors()
			var actual []string
			for _, diagnostic := range errors {
				if diagnostic.Rule == "shellcheck" || diagnostic.Rule == "pyflakes" {
					actual = append(actual, diagnostic.Rule)
				}
			}
			if strings.Join(actual, ",") != tc.want {
				t.Fatalf("external linters %v, want %q: %v", actual, tc.want, errors)
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
