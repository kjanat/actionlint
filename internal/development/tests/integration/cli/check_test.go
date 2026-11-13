package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
	diagnosticmodel "actionlint.kjanat.dev/v2/internal/results"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"

	"actionlint.kjanat.dev/v2/internal/cli"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
	"github.com/google/go-cmp/cmp"
)

func TestCommandWorkflowPaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitRepository(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, ".github", "workflows"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.yml")
	for _, path := range []string{outside, filepath.Join(repo, ".github", "workflows", "ci.yml")} {
		if err := os.WriteFile(path, []byte(commandBadWorkflow), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(repo)
	for _, modern := range []bool{false, true} {
		for _, input := range []string{"", outside, filepath.Join("..", "outside.yml")} {
			t.Run(fmtTestName(modern, input), func(t *testing.T) {
				t.Parallel()
				args := []string{"--json"}
				if input != "" {
					args = append(args, input)
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand("", args...)
				var report resultreport.CheckResult
				if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil || got.Status != 1 || got.Stderr != "" || len(report.Diagnostics) != 1 {
					t.Fatalf("selected workflow was not analyzed: %+v (%v)", got, err)
				}
				want := filepath.Join("..", "outside.yml")
				if input == "" {
					want = filepath.Join(".github", "workflows", "ci.yml")
				}
				if report.Diagnostics[0].Path != want || report.Diagnostics[0].Rule != "expression-reference" {
					t.Fatalf("wrong workflow diagnostic: %+v", report.Diagnostics[0])
				}
			})
		}
	}
}

func TestCLIWriteErrors(t *testing.T) {
	t.Parallel()
	for _, format := range []render.Format{render.FormatText, render.FormatPretty, render.FormatOneline, render.FormatGCC, render.FormatJSON, render.FormatJSONL, render.FormatSARIF, render.FormatGitHub} {
		var stderr strings.Builder
		cmd := cli.Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: commandFailingIO{}, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "--summary=false", "--no-tool=shellcheck", "--no-tool=ruff", "--output-format=" + string(format), "-"})
		if status != 3 || !strings.Contains(stderr.String(), "write failed") {
			t.Fatalf("%s changed CLI streams or status: %d, %s", format, status, &stderr)
		}
	}
}

func TestModernOutputDestinationsAndSummary(t *testing.T) {
	t.Chdir(commandTestRepo(t))
	if err := os.WriteFile("report.json", []byte("previous report"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "check", "--json", "--yes", "--output-file", "report.json", "missing.yml")
	data, err := os.ReadFile("report.json")
	if err != nil || got.Status != 3 || string(data) != "previous report" {
		t.Fatalf("failed check changed output: %+v %s (%v)", got, data, err)
	}
	got = testRunCommand("", "check", "--json", "--summary", "--yes", "--output-file", "report.json")
	data, err = os.ReadFile("report.json")
	var summary struct {
		Summary resultreport.CheckSummary `json:"summary"`
	}
	summaryErr := json.Unmarshal([]byte(got.Stderr), &summary)
	wantSummary := resultreport.CheckSummary{
		Files: 1, Findings: 1,
		ByFile:       []resultreport.FileFindingCount{{Path: filepath.FromSlash(".github/workflows/ci.yml"), Count: 1}},
		ByDiagnostic: []resultreport.DiagnosticCount{{Rule: "expression-reference", Code: "unknown-variable", Count: 1}},
	}
	if err != nil || got.Status != 1 || got.Stdout != "" || !json.Valid(data) || summaryErr != nil || !cmp.Equal(summary.Summary, wantSummary) {
		t.Fatalf("%+v %s (%v)", got, data, err)
	}
	got = testRunCommand("", "check", "--json", "--summary", "--quiet")
	if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || got.Stderr != "" {
		t.Fatal(got)
	}
	got = testRunCommand("", "check", "--yes", "--output-file", ".github/workflows/ci.yml")
	data, err = os.ReadFile(".github/workflows/ci.yml")
	if err != nil || got.Status != 3 || string(data) != commandBadWorkflow {
		t.Fatalf("overwrote workflow: %+v (%v)", got, err)
	}
	artifacts, err := filepath.Glob(".actionlint-report-*")
	if err != nil || len(artifacts) != 0 {
		t.Fatalf("temporary reports remain: %v (%v)", artifacts, err)
	}
	if err := os.WriteFile("report.tmpl", []byte("{{range .}}{{.Kind}}:{{.EndColumn}}{{end}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand("", "check", "--template-file", "report.tmpl")
	expected := testRunCommand("", "-format", "{{range .}}{{.Kind}}:{{.EndColumn}}{{end}}")
	if got != expected {
		t.Fatalf("template file changed legacy fields: %+v / %+v", got, expected)
	}
}

func TestReportMayReplaceUnusedStdinFilename(t *testing.T) {
	t.Parallel()
	dir := commandTestRepo(t)
	report := filepath.Join(dir, "report.json")
	for _, modern := range []bool{false, true} {
		args := []string{"--stdin-filename", report, "--yes", "--output-file", report, "--json", filepath.Join(dir, ".github", "workflows", "ci.yml")}
		if modern {
			args = append([]string{"check"}, args...)
		}
		got := testRunCommand("", args...)
		data, err := os.ReadFile(report)
		if got.Status != 1 || got.Stderr != "" || err != nil || !json.Valid(data) {
			t.Fatalf("unused stdin filename prevented report output: %+v (%v)", got, err)
		}
	}
}

func TestModernRendererAndUsageErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, format := range []string{"text", "oneline", "json", "jsonl", "github", "sarif"} {
		var errout bytes.Buffer
		cmd := cli.Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: commandFailingIO{}, Stderr: &errout}
		if code := cmd.Main([]string{"actionlint", "--summary=false", "check", "-", "--output-format=" + format, "--no-tool=shellcheck", "--no-tool=ruff"}); code != 3 || !strings.Contains(errout.String(), "write failed") {
			t.Fatalf("%s: %d %s", format, code, &errout)
		}
	}
	for _, args := range [][]string{{"check", "--unknown", "--json", "-"}, {"check", "-", "--output-format=csv", "--json"}, {"check", "--json", "--template=x", "-"}, {"config", "show", "--unknown", "--json"}, {"version", "extra"}} {
		if got := testRunCommand(commandGoodWorkflow, args...); got.Status != 2 {
			t.Fatalf("%q: %+v", args, got)
		}
	}
	for _, level := range []string{"none", "info", "debug"} {
		got := testRunCommand(commandBadWorkflow, "check", "-", "--json", "--log-level="+level)
		if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || (got.Stderr == "") != (level == "none") {
			t.Fatal(got)
		}
	}
	for _, mode := range []string{"auto", "always", "never"} {
		var out, errout bytes.Buffer
		cmd := cli.Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: &out, Stderr: &errout}
		status := cmd.Main([]string{"actionlint", "--summary=false", "check", "-", "--json", "--color=" + mode, "--no-tool=shellcheck", "--no-tool=ruff"})
		got := commandTranscript{status, out.String(), errout.String()}
		if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || strings.ContainsRune(got.Stdout, '\x1b') {
			t.Fatal(got)
		}
	}
	if got := testRunCommand(commandBadWorkflow, "check", "-", "--json=false", "--output-format=jsonl"); got.Status != 1 || got.Stderr != "" {
		t.Fatal(got)
	}
	var rules []rule.Descriptor
	metadata := testRunCommand("", "rules", "--json")
	if err := json.Unmarshal([]byte(metadata.Stdout), &rules); err != nil || metadata.Status != 0 {
		t.Fatalf("rule metadata: %+v, %v", metadata, err)
	}
	for _, name := range []string{"syntax-check", "expression-reference", "shellcheck", "unpinned-reference"} {
		if !slices.ContainsFunc(rules, func(r rule.Descriptor) bool { return r.Name == name && r.Description != "" }) {
			t.Errorf("missing rule %q", name)
		}
	}
}

func TestCommandNativeJSON(t *testing.T) {
	t.Parallel()
	for _, input := range []string{commandGoodWorkflow, commandBadWorkflow, ""} {
		legacy := testRunCommand(input, "-format", "{{json .}}", "-")
		for _, args := range [][]string{{"--json", "-"}, {"--output=json", "-"}, {"-o", "json", "-"}} {
			got := testRunCommand(input, args...)
			if got.Status != legacy.Status || got.Stderr != "" {
				t.Fatalf("%q: %+v", args, got)
			}
			var want []*render.TemplateFields
			var have resultreport.CheckResult
			if err := json.Unmarshal([]byte(legacy.Stdout), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(got.Stdout), &have); err != nil {
				t.Fatal(err)
			}
			expected := []diagnosticmodel.Diagnostic{}
			for _, field := range want {
				expected = append(expected, diagnosticmodel.Diagnostic{Rule: field.Kind, Message: field.Message, Path: field.Filepath, Start: sourcepos.Position{Line: field.Line, Column: field.Column}, End: sourcepos.Position{Line: field.Line, Column: field.EndColumn + 1}, Snippet: strings.Split(field.Snippet, "\n")[0]})
			}
			for i := range have.Diagnostics {
				actual := have.Diagnostics[i]
				if actual.Reference == nil || actual.Reference.Revision != sourceid.Digest([]byte(input)) || actual.Help == "" || actual.DocumentationURL == "" {
					t.Fatalf("missing diagnostic context: %+v", actual)
				}
				expected[i].Reference = actual.Reference
				if actual.Rule == "expression-reference" {
					expected[i].Code = "unknown-variable"
				}
				descriptor, ok := checking.FindRule(actual.Rule)
				if !ok {
					t.Fatalf("unknown rule %s", actual.Rule)
				}
				expected[i].Help = descriptor.Help
				expected[i].Explanation = descriptor.Explanation
				expected[i].DocumentationURL = descriptor.DocumentationURL
			}
			if diff := cmp.Diff(expected, have.Diagnostics); diff != "" {
				t.Fatal(diff)
			}
			if len(have.Diagnostics) == 0 && !cleanCheckJSON(got.Stdout) {
				t.Fatalf("empty diagnostics: %q", got.Stdout)
			}
		}
		lines := testRunCommand(input, "--output=jsonl", "-")
		array := testRunCommand(input, "--json", "-")
		var result resultreport.CheckResult
		if err := json.Unmarshal([]byte(array.Stdout), &result); err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		for _, diagnostic := range result.Diagnostics {
			if err := json.NewEncoder(&expected).Encode(struct {
				SchemaVersion int `json:"schema_version"`
				diagnosticmodel.Diagnostic
			}{1, diagnostic}); err != nil {
				t.Fatal(err)
			}
		}
		if lines.Status != array.Status || lines.Stderr != "" || lines.Stdout != expected.String() {
			t.Fatalf("JSONL differs from JSON diagnostics: %+v", lines)
		}
	}
}

func TestCommandMultiFileJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "one.yml"), filepath.Join(dir, "two.yml")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(commandBadWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := testRunCommand("", "--json", paths[0], paths[1])
	var result resultreport.CheckResult
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
		t.Fatal(err)
	}
	fields := result.Diagnostics
	if got.Status != 1 || got.Stderr != "" || len(fields) != 2 {
		t.Fatalf("%+v", got)
	}
	for i, path := range paths {
		if fields[i].Path != workingDirectoryPath(t, path) || fields[i].Start.Line != 6 || fields[i].Rule != "expression-reference" || !strings.Contains(fields[i].Snippet, "missing.value") {
			t.Errorf("wrong diagnostic: %+v", fields[i])
		}
	}
}

func TestCommandOutputSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode string
		args []string
	}{
		{"pretty", nil},
		{"oneline", []string{"-oneline"}},
		{"text", []string{"check", "--output", "text"}},
	} {
		legacy := testRunCommand(commandBadWorkflow, append(tc.args, "-")...)
		got := testRunCommand(commandBadWorkflow, "--output", tc.mode, "-")
		if diff := cmp.Diff(legacy, got); diff != "" {
			t.Fatalf("%s: %s", tc.mode, diff)
		}
	}
}

func TestApplicationBuiltInOutput(t *testing.T) {
	t.Parallel()
	for _, format := range []render.Format{render.FormatText, render.FormatOneline, render.FormatJSON, render.FormatJSONL, render.FormatSARIF} {
		var out bytes.Buffer
		result, err := checking.Check(t.Context(), checking.Options{}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(commandBadWorkflow)})
		if err != nil || len(result.Diagnostics) != 1 {
			t.Fatalf("%s: %v, %v", format, result, err)
		}
		renderer, err := render.New(format, "", false, buildinfo.Version, nil)
		if err != nil {
			t.Fatal(err)
		}
		presentation := render.FromAnalysis(*result)
		presentation.CheckResult = commandstatus.AnalysisResult(*result)
		if err := renderer.Render(&out, presentation); err != nil {
			t.Fatal(err)
		}
		cli := testRunCommand(commandBadWorkflow, "--output", string(format), "-")
		if out.String() != cli.Stdout {
			t.Fatalf("Go API and CLI differ for %s: %s", format, cmp.Diff(cli.Stdout, out.String()))
		}
	}
	for _, opts := range []struct {
		format   render.Format
		template string
	}{{"csv", ""}, {render.FormatJSON, "{{json .}}"}} {
		if _, err := render.New(opts.format, opts.template, false, buildinfo.Version, nil); err == nil {
			t.Fatalf("invalid output options accepted: %+v", opts)
		}
	}
}

func TestReportCannotReplaceConsumedFile(t *testing.T) {
	t.Parallel()
	for _, modern := range []bool{false, true} {
		for _, kind := range []string{"stdin", "action", "reusable", "hardlink", "symlink"} {
			t.Run(fmtTestName(modern, kind), func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				gitRepository(t, root)
				for _, dir := range []string{".github/workflows", "local"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
				input := filepath.Join(root, ".github", "workflows", "input.yml")
				target := input
				workflow := commandGoodWorkflow
				content := workflow
				switch kind {
				case "action", "hardlink", "symlink":
					target = filepath.Join(root, "local", "action.yml")
					content = `name: local
runs:
  using: composite
  steps:
    - run: echo ok
      shell: bash
`
					workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local
permissions: {}
`
				case "reusable":
					target = filepath.Join(root, ".github", "workflows", "called.yml")
					content = strings.Replace(commandGoodWorkflow, "on: push", "on: workflow_call", 1)
					workflow = `on: push
jobs:
  test:
    uses: ./.github/workflows/called.yml
permissions: {}
`
				}
				if err := os.WriteFile(target, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				report := target
				if kind == "hardlink" || kind == "symlink" {
					report = filepath.Join(root, "alias.yml")
					var err error
					if kind == "hardlink" {
						err = os.Link(target, report)
					} else {
						err = os.Symlink(target, report)
					}
					if err != nil {
						t.Skip(err)
					}
				}
				args := []string{"--yes", "--output-file", report, "--stdin-filename", target, "-"}
				stdin := workflow
				if kind != "stdin" {
					if err := os.WriteFile(input, []byte(workflow), 0644); err != nil {
						t.Fatal(err)
					}
					args = []string{"--yes", "--output-file", report, input}
					stdin = ""
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand(stdin, args...)
				if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
					t.Fatalf("%+v", got)
				}
				after, err := os.ReadFile(target)
				if err != nil || string(after) != content {
					t.Fatalf("input changed: %q, %v", after, err)
				}
			})
		}
	}
}

func TestReportMayReplaceUnselectedConfig(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmtTestName(modern, "config"), func(t *testing.T) {
			t.Chdir(commandTestRepo(t))
			selected := filepath.Join(".github", "actionlint.yaml")
			if err := os.WriteFile(selected, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			unused := filepath.Join(".github", "actionlint.yml")
			args := []string{"--json", "--yes", "--output-file", unused}
			if modern {
				args = append([]string{"check"}, args...)
			}
			got := testRunCommand("", args...)
			data, err := os.ReadFile(unused)
			if got.Status != 1 || got.Stderr != "" || err != nil || !json.Valid(data) {
				t.Fatalf("unused config name prevented report output: %+v (%v)", got, err)
			}
			args[len(args)-1] = selected
			got = testRunCommand("", args...)
			data, err = os.ReadFile(selected)
			if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") || err != nil || string(data) != "{}\n" {
				t.Fatalf("selected config was not protected: %+v (%v)", got, err)
			}
		})
	}
}

func TestReportPreservesPermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	report := filepath.Join(t.TempDir(), "report")
	if err := os.WriteFile(report, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(commandGoodWorkflow, "check", "--yes", "--output-file", report, "--no-config", "-")
	if got.Status != 0 {
		t.Fatal(got)
	}
	info, err := os.Stat(report)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
}

func cleanCheckJSON(data string) bool {
	var result resultreport.CheckResult
	return json.Unmarshal([]byte(data), &result) == nil && result.SchemaVersion == 1 &&
		result.Completed && result.Status == "success" && result.ExitCode == 0 &&
		result.FileCount != nil && *result.FileCount == 1 && result.Diagnostics != nil && len(result.Diagnostics) == 0
}

func TestCheckJSONFailureContract(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(commandTestRepo(t), "missing.yml")
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"check", "--json", missing}, 3},
		{[]string{"check", "--json", "--does-not-exist"}, 2},
		{[]string{"--json", missing}, 3},
	} {
		got := testRunCommand("", tc.args...)
		var result resultreport.CheckResult
		if err := json.Unmarshal([]byte(got.Stderr), &result); err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.code || result.ExitCode != tc.code || result.Completed || result.SchemaVersion != 1 || result.Error == "" {
			t.Fatalf("invalid failure report: %+v", got)
		}
	}
	for _, name := range []string{"config", "doctor", "rules", "version"} {
		got := testRunCommand("", name, "--json", "--does-not-exist")
		if got.Status != 2 || strings.Contains(got.Stderr, "schema_version") || !json.Valid([]byte(got.Stderr)) {
			t.Fatalf("metadata error changed: %+v", got)
		}
	}
}

func TestJSONErrorsBeforeOperationSelection(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--version", "--json", "--does-not-exist"},
		{"--json", "--does-not-exist", "--version"},
		{"--init-config", "--json", "--does-not-exist"},
		{"--completion=bash", "--json", "--does-not-exist"},
		{"--json", "--completion=invalid"},
		{"--json", "--does-not-exist"},
	} {
		got := testRunCommand("", args...)
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(got.Stderr), &record); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if got.Status != 2 || got.Stdout != "" || len(record) != 2 || string(record["exit_code"]) != "2" || len(record["error"]) <= 2 {
			t.Errorf("%q: want invocation error, got %+v", args, got)
		}
	}
}
