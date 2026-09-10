package actionlint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func commandTestRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, path := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestModernCommandDispatch(t *testing.T) {
	commandTestRepo(t)
	if err := os.WriteFile("good.yml", []byte(commandGoodWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"check", "good.yml", "--json"},
		{"check", "--json", "good.yml"},
		{"check", "-ojson", "good.yml"},
		{"check", "good.yml", "--output-format=json"},
	} {
		got := testRunCommand("", args...)
		if got.Status != 0 || got.Stdout != "{\"schema_version\":1,\"diagnostics\":[]}\n" || got.Stderr != "" {
			t.Fatalf("%q: %+v", args, got)
		}
	}
	legacy := testRunCommand("", "good.yml", "--json")
	if legacy.Status != 3 || !strings.Contains(legacy.Stderr, `"--json"`) {
		t.Fatalf("root parsed a filename as a flag: %+v", legacy)
	}
	for _, name := range []string{"check", "config", "rules", "doctor", "completion", "version"} {
		got := testRunCommand("", "-version", name)
		want := testRunCommand("", "-version")
		if got != want {
			t.Fatalf("subcommand bypassed legacy version precedence: %+v / %+v", got, want)
		}
	}
	directory := testRunCommand("", "check", ".github/workflows")
	if directory.Status != 3 {
		t.Fatalf("directory input was expanded: %+v", directory)
	}
	for _, name := range []string{"check", "config", "rules", "doctor", "completion", "version", "help", "__complete"} {
		if err := os.WriteFile(name, []byte(commandGoodWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{name}, {"--", name}} {
			if got := testRunCommand("", args...); got != (commandTranscript{}) {
				t.Fatalf("command stole filename %q: %+v", name, got)
			}
		}
	}
	forced := testRunCommand("", "--command", "version", "--json")
	var build commandBuildInfo
	if err := json.Unmarshal([]byte(forced.Stdout), &build); err != nil || forced.Status != 0 || build.Version == "" {
		t.Fatalf("command escape: %+v (%v)", forced, err)
	}
	for _, args := range [][]string{{"--command", "check", "--color=never", "-ojson", "good.yml"}, {"--command=check", "--json", "good.yml"}} {
		if got := testRunCommand("", args...); got.Status != 0 || got.Stdout != "{\"schema_version\":1,\"diagnostics\":[]}\n" || got.Stderr != "" {
			t.Fatalf("command escape used legacy grammar: %q: %+v", args, got)
		}
	}
	unknown := testRunCommand("", "--command", "not-a-command")
	if unknown.Status != 2 {
		t.Fatal(unknown)
	}
}

func TestModernRepeatedIgnoreAliases(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--ignore-regex", "undefined variable", "--ignore", "other", "-"},
		{"check", "--ignore", "undefined variable", "--ignore-regex", "other", "-"},
		{"--ignore", "undefined variable", "check", "--ignore-regex", "other", "-"},
	} {
		if got := testRunCommand(commandBadWorkflow, args...); got != (commandTranscript{}) {
			t.Fatalf("repeated patterns were lost: %q: %+v", args, got)
		}
	}
}

func TestModernConfigCreationAndYAMLOrigins(t *testing.T) {
	commandTestRepo(t)
	for _, operation := range [][]string{{"-init-config"}, {"config", "init"}, {"version"}, {"rules"}} {
		args := append([]string{"--output-file", ".github/actionlint.yaml"}, operation...)
		if got := testRunCommand("", args...); got.Status != 2 {
			t.Fatalf("accepted check output destination for %q: %+v", operation, got)
		}
		if _, err := os.Stat(".github/actionlint.yaml"); !os.IsNotExist(err) {
			t.Fatalf("invalid output destination created config: %v", err)
		}
	}
	created := testRunCommand("", "config", "init", "--json")
	var result map[string]string
	if err := json.Unmarshal([]byte(created.Stdout), &result); err != nil || created.Status != 0 {
		t.Fatalf("%+v (%v)", created, err)
	}
	data, err := os.ReadFile(result["path"])
	if err != nil || !bytes.Contains(data, []byte("yaml-language-server: $schema=")) {
		t.Fatalf("%s (%v)", data, err)
	}
	if got := testRunCommand("", "config", "show"); got.Status != 0 || !strings.Contains(got.Stdout, "config-variables: null") {
		t.Fatal(got)
	}
	config := "defaults: &defaults {config-variables: []}\n<<: *defaults\nconfig-secrets: null\n"
	if err := os.WriteFile(result["path"], []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--origin", "--json")
	var inspection configInspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	if inspection.Origins["/config-variables"].Source != "config" || inspection.Origins["/config-secrets"].State != "null" {
		t.Fatal(inspection.Origins)
	}
	if _, exists := inspection.Origins["/defaults"]; exists {
		t.Fatal("ignored setting reported as effective")
	}
}

func TestModernConfigSelectionAndOrigins(t *testing.T) {
	commandTestRepo(t)
	config := "config-variables: []\nconfig-secrets: null\npolicy:\n  require-commit-hash: false\n  required-actions: []\n  require-job-timeout: {min-minutes: 2, max-minutes: 10}\n"
	if err := os.WriteFile(".github/actionlint.yml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--json", "--origin")
	var inspection configInspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	for key, state := range map[string]string{"/config-variables": "value", "/config-secrets": "null", "/policy/require-commit-hash": "value", "/policy/required-actions": "value"} {
		origin := inspection.Origins[key]
		if origin.Source != "config" || origin.State != state || origin.Line == 0 {
			t.Errorf("%s: %+v", key, origin)
		}
	}
	if inspection.Origins["/assume-default-permissions"].Source != "default" {
		t.Fatal(inspection.Origins)
	}
	if inspection.Config["config-variables"] == nil || inspection.Config["config-secrets"] != nil {
		t.Fatal("[] and null lost their distinction")
	}
	policy := inspection.Config["policy"].(map[string]any)
	if policy["require-commit-hash"] != false || policy["required-actions"] == nil {
		t.Fatal(policy)
	}
	for _, args := range [][]string{{"config", "validate"}, {"config", "path"}, {"config", "show"}} {
		if got := testRunCommand("", args...); got.Status != 0 || got.Stderr != "" || got.Stdout == "" {
			t.Fatal(got)
		}
	}
	if err := os.WriteFile(".github/actionlint.yaml", []byte("policy: {unknown: true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := testRunCommand("", "config", "validate", "--json"); got.Status != 3 || !strings.Contains(got.Stderr, "unknown key") {
		t.Fatal(got)
	}
	if got := testRunCommand("", "config", "path"); got.Status != 0 || !strings.Contains(got.Stdout, "actionlint.yaml") {
		t.Fatal(got)
	}
	if got := testRunCommand("", "config", "init"); got.Status != 3 || !strings.Contains(got.Stderr, "already exists") {
		t.Fatal(got)
	}
	for _, args := range [][]string{
		{"check", "--no-config", "--json"},
		{"check", "--config", ".github/actionlint.yml", "--json"},
	} {
		got := testRunCommand("", args...)
		if got.Status != 1 || got.Stderr != "" || !json.Valid([]byte(got.Stdout)) {
			t.Fatalf("explicit selection loaded invalid repository config: %+v", got)
		}
	}
	conflict := testRunCommand("", "check", "--config", "x.yml", "--no-config")
	if conflict.Status != 2 {
		t.Fatal(conflict)
	}
}

func TestModernInformationCommandsDoNotRunAnalysis(t *testing.T) {
	commandTestRepo(t)
	if err := os.WriteFile(".github/actionlint.yaml", []byte("policy: {bad: true}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"check", "--help"}, {"help", "check"}, {"version"}, {"version", "--json"}, {"rules"}, {"rules", "expression", "--json"}, {"completion", "zsh"}, {"--help-legacy"}} {
		if got := testRunCommand("", args...); got.Status != 0 {
			t.Fatalf("%q loaded configuration: %+v", args, got)
		}
	}
	if got := testRunCommand("", "rules", "nonexistent"); got.Status != 2 {
		t.Fatal(got)
	}
	got := testRunCommand("", "doctor", "--no-config", "--json", "--shellcheck", "this-tool-does-not-exist", "--pyflakes=")
	var report struct {
		Tools []doctorTool `json:"tools"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	if len(report.Tools) != 2 || report.Tools[0].Status != "unavailable" || report.Tools[1].Status != "disabled" {
		t.Fatal(report.Tools)
	}
	if got := testRunCommand("", "doctor", "--json"); got.Status != 3 || !json.Valid([]byte(got.Stdout)) {
		t.Fatal(got)
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
	var out bytes.Buffer
	fields := []*ErrorTemplateFields{{Filepath: "a%,:\r\nb.yml", Message: "bad%\r\n::notice::text", Kind: "expression", Line: 2, Column: 3, EndColumn: 4}}
	if err := (githubDiagnosticFormatter{}).Print(&out, fields); err != nil {
		t.Fatal(err)
	}
	want := "::error file=a%25%2C%3A%0D%0Ab.yml,line=2,col=3,endColumn=4,title=expression::bad%25%0D%0A::notice::text\n"
	if out.String() != want {
		t.Fatalf("annotation escaping: %q", out.String())
	}
	rules := commandRules()
	for _, name := range []string{"syntax-check", "expression", "shellcheck", "pyflakes", "require-commit-hash"} {
		if !slices.ContainsFunc(rules, func(r ruleDescriptor) bool { return r.Name == name && r.Description != "" }) {
			t.Errorf("missing rule %q", name)
		}
	}
}
