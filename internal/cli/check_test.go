package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
)

func TestCommandWorkflowPaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	for _, dir := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, ".github", "workflows")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
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
				args := []string{"--json"}
				if input != "" {
					args = append(args, input)
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand("", args...)
				var report actionlint.CheckResult
				if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil || got.Status != 1 || got.Stderr != "" || len(report.Diagnostics) != 1 {
					t.Fatalf("selected workflow was not analyzed: %+v (%v)", got, err)
				}
				want := filepath.Join("..", "outside.yml")
				if input == "" {
					want = filepath.Join(".github", "workflows", "ci.yml")
				}
				if report.Diagnostics[0].Path != want || report.Diagnostics[0].Rule != "expression" {
					t.Fatalf("wrong workflow diagnostic: %+v", report.Diagnostics[0])
				}
			})
		}
	}
}

func TestLegacyCLITextWriteErrors(t *testing.T) {
	for _, format := range []actionlint.OutputFormat{actionlint.OutputFormatText, actionlint.OutputFormatOneline, actionlint.OutputFormatJSON, actionlint.OutputFormatJSONL, actionlint.OutputFormatSARIF, actionlint.OutputFormatGitHub} {
		text := format == actionlint.OutputFormatText || format == actionlint.OutputFormatOneline
		var stderr strings.Builder
		cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: commandFailingIO{}, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "--shellcheck=", "--pyflakes=", "--output-format=" + string(format), "-"})
		if text && (status != 1 || stderr.Len() != 0) || !text && (status != 3 || !strings.Contains(stderr.String(), "write failed")) {
			t.Fatalf("%s changed CLI streams or status: %d, %s", format, status, &stderr)
		}
	}
}

func TestModernOutputDestinationsAndSummary(t *testing.T) {
	commandTestRepo(t)
	if err := os.WriteFile("report.json", []byte("previous report"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "check", "--json", "--output-file", "report.json", "missing.yml")
	data, err := os.ReadFile("report.json")
	if err != nil || got.Status != 3 || string(data) != "previous report" {
		t.Fatalf("failed check changed output: %+v %s (%v)", got, data, err)
	}
	got = testRunCommand("", "check", "--json", "--summary", "--output-file", "report.json")
	data, err = os.ReadFile("report.json")
	if err != nil || got.Status != 1 || got.Stdout != "" || !json.Valid(data) || got.Stderr != "{\"summary\":{\"files\":1,\"findings\":1}}\n" {
		t.Fatalf("%+v %s (%v)", got, data, err)
	}
	got = testRunCommand("", "check", "--json", "--summary", "--quiet")
	if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || got.Stderr != "" {
		t.Fatal(got)
	}
	got = testRunCommand("", "check", "--output-file", ".github/workflows/ci.yml")
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

func TestModernRendererAndUsageErrors(t *testing.T) {
	oldColor := color.NoColor
	t.Cleanup(func() { color.NoColor = oldColor })
	for _, format := range []string{"text", "oneline", "json", "jsonl", "github", "sarif"} {
		var errout bytes.Buffer
		cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: commandFailingIO{}, Stderr: &errout}
		if code := cmd.Main([]string{"actionlint", "check", "-", "--output-format=" + format, "--shellcheck=", "--pyflakes="}); code != 3 || !strings.Contains(errout.String(), "write failed") {
			t.Fatalf("%s: %d %s", format, code, &errout)
		}
	}
	for _, args := range [][]string{{"check", "--unknown", "--json", "-"}, {"check", "-", "--output-format=csv", "--json"}, {"check", "--json", "--template=x", "-"}, {"config", "show", "--unknown", "--json"}, {"completion"}, {"version", "extra"}} {
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
		cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: &out, Stderr: &errout}
		status := cmd.Main([]string{"actionlint", "check", "-", "--json", "--color=" + mode, "--shellcheck=", "--pyflakes="})
		got := commandTranscript{status, out.String(), errout.String()}
		if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || strings.ContainsRune(got.Stdout, '\x1b') {
			t.Fatal(got)
		}
	}
	if got := testRunCommand(commandBadWorkflow, "check", "-", "--json=false", "--output-format=jsonl"); got.Status != 1 || got.Stderr != "" {
		t.Fatal(got)
	}
	rules := commandRules()
	for _, name := range []string{"syntax-check", "expression", "shellcheck", "pyflakes", "require-commit-hash"} {
		if !slices.ContainsFunc(rules, func(r actionlint.RuleInfo) bool { return r.Name == name && r.Description != "" }) {
			t.Errorf("missing rule %q", name)
		}
	}
}

func TestCommandNativeJSON(t *testing.T) {
	for _, input := range []string{commandGoodWorkflow, commandBadWorkflow, ""} {
		legacy := testRunCommand(input, "-format", "{{json .}}", "-")
		for _, args := range [][]string{{"--json", "-"}, {"--output=json", "-"}, {"-o", "json", "-"}} {
			got := testRunCommand(input, args...)
			if got.Status != legacy.Status || got.Stderr != "" {
				t.Fatalf("%q: %+v", args, got)
			}
			var want []*actionlint.ErrorTemplateFields
			var have actionlint.CheckResult
			if err := json.Unmarshal([]byte(legacy.Stdout), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(got.Stdout), &have); err != nil {
				t.Fatal(err)
			}
			expected := actionlint.CheckResult{SchemaVersion: 1, Diagnostics: []actionlint.Diagnostic{}}
			for _, field := range want {
				expected.Diagnostics = append(expected.Diagnostics, actionlint.Diagnostic{Rule: field.Kind, Message: field.Message, Path: field.Filepath, Start: actionlint.DiagnosticPosition{Line: field.Line, Column: field.Column}, End: actionlint.DiagnosticPosition{Line: field.Line, Column: field.EndColumn + 1}, Snippet: strings.Split(field.Snippet, "\n")[0]})
			}
			if diff := cmp.Diff(expected, have); diff != "" {
				t.Fatal(diff)
			}
			if len(have.Diagnostics) == 0 && got.Stdout != "{\"schema_version\":1,\"diagnostics\":[]}\n" {
				t.Fatalf("empty diagnostics: %q", got.Stdout)
			}
		}
		lines := testRunCommand(input, "--output=jsonl", "-")
		array := testRunCommand(input, "--json", "-")
		var result actionlint.CheckResult
		if err := json.Unmarshal([]byte(array.Stdout), &result); err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		for _, diagnostic := range result.Diagnostics {
			if err := json.NewEncoder(&expected).Encode(struct {
				SchemaVersion int `json:"schema_version"`
				actionlint.Diagnostic
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
	t.Chdir(t.TempDir())
	for _, path := range []string{"one.yml", "two.yml"} {
		if err := os.WriteFile(path, []byte(commandBadWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := testRunCommand("", "--json", "one.yml", "two.yml")
	var result actionlint.CheckResult
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
		t.Fatal(err)
	}
	fields := result.Diagnostics
	if got.Status != 1 || got.Stderr != "" || len(fields) != 2 {
		t.Fatalf("%+v", got)
	}
	for i, path := range []string{"one.yml", "two.yml"} {
		if fields[i].Path != path || fields[i].Start.Line != 6 || fields[i].Rule != "expression" || !strings.Contains(fields[i].Snippet, "missing.value") {
			t.Errorf("wrong diagnostic: %+v", fields[i])
		}
	}
}

func TestCommandOutputSelection(t *testing.T) {
	for _, mode := range []string{"text", "oneline", "sarif"} {
		args := []string{}
		switch mode {
		case "oneline":
			args = append(args, "-oneline")
		case "sarif":
			args = append(args, "-format", actionlint.SARIFTemplate())
		}
		legacy := testRunCommand(commandBadWorkflow, append(args, "-")...)
		got := testRunCommand(commandBadWorkflow, "--output", mode, "-")
		if diff := cmp.Diff(legacy, got); diff != "" {
			t.Fatalf("%s: %s", mode, diff)
		}
	}
}

func TestLinterBuiltInOutput(t *testing.T) {
	for _, format := range []actionlint.OutputFormat{actionlint.OutputFormatText, actionlint.OutputFormatOneline, actionlint.OutputFormatJSON, actionlint.OutputFormatJSONL, actionlint.OutputFormatSARIF} {
		var out bytes.Buffer
		linter, err := actionlint.NewLinter(&out, &actionlint.LinterOptions{Color: actionlint.ColorOptionKindNever, OutputFormat: format})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := linter.Lint("<stdin>", []byte(commandBadWorkflow), nil)
		if err != nil || len(findings) != 1 {
			t.Fatalf("%s: %v, %v", format, findings, err)
		}
		cli := testRunCommand(commandBadWorkflow, "--output", string(format), "-")
		if out.String() != cli.Stdout {
			t.Fatalf("Go API and CLI differ for %s: %s", format, cmp.Diff(cli.Stdout, out.String()))
		}
	}
	for _, opts := range []actionlint.LinterOptions{{OutputFormat: "csv"}, {OutputFormat: actionlint.OutputFormatJSON, Format: "{{json .}}"}} {
		if _, err := actionlint.NewLinter(io.Discard, &opts); err == nil {
			t.Fatalf("invalid output options accepted: %+v", opts)
		}
	}
}

func TestReportCannotReplaceConsumedFile(t *testing.T) {
	for _, modern := range []bool{false, true} {
		for _, kind := range []string{"stdin", "action", "reusable", "hardlink", "symlink"} {
			t.Run(fmtTestName(modern, kind), func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				for _, dir := range []string{".git", ".github/workflows", "local"} {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				target := ".github/workflows/input.yml"
				workflow := commandGoodWorkflow
				content := workflow
				switch kind {
				case "action", "hardlink", "symlink":
					target = "local/action.yml"
					content = "name: local\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
					workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n"
				case "reusable":
					target = ".github/workflows/called.yml"
					content = strings.Replace(commandGoodWorkflow, "on: push", "on: workflow_call", 1)
					workflow = "on: push\njobs:\n  test:\n    uses: ./.github/workflows/called.yml\n"
				}
				if err := os.WriteFile(target, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				report := target
				if kind == "hardlink" || kind == "symlink" {
					report = "alias.yml"
					var err error
					if kind == "hardlink" {
						err = os.Link(target, report)
					} else {
						err = os.Symlink(filepath.Join(root, target), report)
					}
					if err != nil {
						t.Skip(err)
					}
				}
				args := []string{"--output-file", report, "--stdin-filename", target, "-"}
				input := workflow
				if kind != "stdin" {
					if err := os.WriteFile(".github/workflows/input.yml", []byte(workflow), 0644); err != nil {
						t.Fatal(err)
					}
					args = []string{"--output-file", report, ".github/workflows/input.yml"}
					input = ""
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand(input, args...)
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

func TestReportPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("report", []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(commandGoodWorkflow, "check", "--output-file", "report", "-")
	if got.Status != 0 {
		t.Fatal(got)
	}
	info, err := os.Stat("report")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
}
