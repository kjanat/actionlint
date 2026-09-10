package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestCommandStreamsAndContext(t *testing.T) {
	var out, errout bytes.Buffer
	cmd := Command{Stdin: commandFailingIO{}, Stdout: &out, Stderr: &errout}
	if code := cmd.Main([]string{"actionlint", "--json", "-"}); code != 3 || !strings.Contains(errout.String(), "read failed") {
		t.Fatalf("%d %s", code, &errout)
	}
	for _, args := range [][]string{{"--json", "-"}, {"--json", "--help"}, {"--version"}, {"--completion", "bash"}} {
		errout.Reset()
		cmd = Command{Stdin: strings.NewReader(commandGoodWorkflow), Stdout: commandFailingIO{}, Stderr: &errout}
		if code := cmd.Main(append([]string{"actionlint"}, args...)); code != 3 || !strings.Contains(errout.String(), "write failed") {
			t.Fatalf("%q: %d %s", args, code, &errout)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	errout.Reset()
	cmd = Command{Stdout: &out, Stderr: &errout}
	if code := cmd.MainContext(ctx, []string{"actionlint", "--json", "-"}); code != 3 || !strings.Contains(errout.String(), "context canceled") {
		t.Fatalf("%d %s", code, &errout)
	}
	cmd = Command{}
	for _, args := range [][]string{{"actionlint", "--version"}, {"actionlint", "--help"}} {
		if code := cmd.Main(args); code != 0 {
			t.Fatalf("nil streams: %q -> %d", args, code)
		}
	}
	if code := cmd.Main([]string{"actionlint", "--json", "-"}); code != 1 {
		t.Fatalf("empty stdin should report a syntax finding: %d", code)
	}
}

func TestCommandMain(t *testing.T) {
	var output bytes.Buffer

	// Create command instance populating stdin/stdout/stderr
	cmd := Command{
		Stdin:  os.Stdin,
		Stdout: &output,
		Stderr: &output,
	}

	// Run the command end-to-end. Note that given args should contain program name
	workflow := filepath.Join("testdata", "examples", "main.yaml")
	status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-ignore", `label .+ is unknown\.`, workflow})

	if status != 1 {
		t.Fatal("exit status should be 1 but got", status)
	}

	out := output.String()

	for _, s := range []string{
		"main.yaml:3:5:",
		"unexpected key \"branch\" for \"push\" section",
		"^~~~~~~~~~~~~~~",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output should contain %q: %q", s, out)
		}
	}

	if strings.Contains(out, "[runner-label]") {
		t.Errorf("runner-label rule should be ignored by -ignore but it is included in output: %q", out)
	}
}
