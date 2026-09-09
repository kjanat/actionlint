package actionlint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const commandGoodWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
const commandBadWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"

func testRunCommand(input string, args ...string) commandTranscript {
	var out, errout bytes.Buffer
	cmd := Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errout}
	status := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--pyflakes=", "--no-color"}, args...))
	return commandTranscript{status, out.String(), errout.String()}
}

func TestCommandArgumentCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		status   int
		contains string
	}{
		{"long", []string{"--oneline", "-"}, 1, "[expression]"},
		{"legacy", []string{"-oneline", "-"}, 1, "[expression]"},
		{"short format", []string{"-f", "{{range .}}{{.Kind}}{{end}}", "-"}, 1, "expression"},
		{"short output", []string{"-ojson", "-"}, 1, `"kind":"expression"`},
		{"option-like value", []string{"--stdin-filename", "-completions", "-"}, 1, "-completions:6:"},
		{"option-like equal value", []string{"-stdin-filename=-completions", "-"}, 1, "-completions:6:"},
		{"ignore literal flag", []string{"-ignore", "--json", "-"}, 1, "[expression]"},
		{"true value", []string{"--oneline=TRUE", "-"}, 1, "[expression]"},
		{"unknown", []string{"--unknown"}, 2, "unknown flag"},
		{"missing value", []string{"--output"}, 2, "needs an argument"},
		{"invalid bool", []string{"--oneline=maybe"}, 2, "invalid syntax"},
		{"unknown shell", []string{"--completion", "not-a-shell"}, 2, "shell"},
		{"help exits early", []string{"--help", "--unknown"}, 0, "Usage:"},
		{"legacy help value", []string{"-help=false"}, 0, "Usage:"},
		{"help short", []string{"-h"}, 0, "Usage:"},
		{"invalid before help", []string{"--unknown", "--help"}, 2, "unknown flag"},
		{"format conflict", []string{"--json", "--format={{json .}}", "-"}, 2, "cannot be combined"},
		{"output conflict", []string{"--json", "--output=jsonl", "-"}, 2, "cannot be combined"},
		{"bad output", []string{"--output=csv", "-"}, 2, "invalid output mode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testRunCommand(commandBadWorkflow, tc.args...)
			if got.Status != tc.status || !strings.Contains(got.Stdout+got.Stderr, tc.contains) {
				t.Fatalf("got %+v, want status=%d containing %q", got, tc.status, tc.contains)
			}
		})
	}
}

func TestCommandLiteralFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"first.yml", "--json", "-completions", "--help", "version", "completion", "__complete"} {
		if err := os.WriteFile(name, []byte(commandGoodWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"first.yml", "--json", "-completions", "--help"},
		{"--", "--json", "--help", "-completions", "__complete"},
		{"version", "completion"},
	} {
		got := testRunCommand("", args...)
		if got != (commandTranscript{}) {
			t.Errorf("%q: %+v", args, got)
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
			var want, have []ErrorTemplateFields
			if err := json.Unmarshal([]byte(legacy.Stdout), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(got.Stdout), &have); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, have); diff != "" {
				t.Fatal(diff)
			}
			if len(have) == 0 && got.Stdout != "[]\n" {
				t.Fatalf("empty diagnostics: %q", got.Stdout)
			}
		}
		legacyLines := testRunCommand(input, "-format", "{{range .}}{{json .}}{{end}}", "-")
		lines := testRunCommand(input, "--output=jsonl", "-")
		if diff := cmp.Diff(legacyLines, lines); diff != "" {
			t.Fatal(diff)
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
	var fields []ErrorTemplateFields
	if err := json.Unmarshal([]byte(got.Stdout), &fields); err != nil {
		t.Fatal(err)
	}
	if got.Status != 1 || got.Stderr != "" || len(fields) != 2 {
		t.Fatalf("%+v", got)
	}
	for i, path := range []string{"one.yml", "two.yml"} {
		if fields[i].Filepath != path || fields[i].Line != 6 || fields[i].Kind != "expression" || !strings.Contains(fields[i].Snippet, "missing.value") {
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
			args = append(args, "-format", SARIFTemplate())
		}
		legacy := testRunCommand(commandBadWorkflow, append(args, "-")...)
		got := testRunCommand(commandBadWorkflow, "--output", mode, "-")
		if diff := cmp.Diff(legacy, got); diff != "" {
			t.Fatalf("%s: %s", mode, diff)
		}
	}
}

func TestLinterBuiltInOutput(t *testing.T) {
	for _, format := range []OutputFormat{OutputFormatText, OutputFormatOneline, OutputFormatJSON, OutputFormatJSONL, OutputFormatSARIF} {
		var out bytes.Buffer
		linter, err := NewLinter(&out, &LinterOptions{Color: ColorOptionKindNever, OutputFormat: format})
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
	for _, opts := range []LinterOptions{{OutputFormat: "csv"}, {OutputFormat: OutputFormatJSON, Format: "{{json .}}"}} {
		if _, err := NewLinter(io.Discard, &opts); err == nil {
			t.Fatalf("invalid output options accepted: %+v", opts)
		}
	}
}

func TestCommandJSONErrorsAndLogs(t *testing.T) {
	tests := []struct {
		args   []string
		status int
	}{
		{[]string{"--json", "--unknown"}, 2},
		{[]string{"--unknown", "--json"}, 2},
		{[]string{"--unknown", "--output", "json"}, 2},
		{[]string{"--unknown", "-ojson"}, 2},
		{[]string{"--json", "--ignore", "[", "-"}, 3},
		{[]string{"--output=jsonl", "missing.yml"}, 3},
		{[]string{"--json", "--debug", "--config-file=does-not-exist.yml", "-"}, 3},
	}
	for _, tc := range tests {
		got := testRunCommand(commandGoodWorkflow, tc.args...)
		var message struct {
			Error    string `json:"error"`
			ExitCode int    `json:"exit_code"`
		}
		if err := json.Unmarshal([]byte(got.Stderr), &message); err != nil {
			t.Fatalf("%q: %+v: %v", tc.args, got, err)
		}
		if got.Stdout != "" || got.Status != tc.status || message.ExitCode != tc.status || message.Error == "" {
			t.Fatalf("%q: %+v", tc.args, got)
		}
	}
	for _, flag := range []string{"--verbose", "--debug"} {
		got := testRunCommand(commandBadWorkflow, "--json", flag, "-")
		if !json.Valid([]byte(got.Stdout)) || got.Stderr == "" {
			t.Fatalf("%+v", got)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(got.Stderr), "\n") {
			var record map[string]string
			if err := json.Unmarshal([]byte(line), &record); err != nil || record["log"] == "" {
				t.Fatalf("not a JSON log record: %q (%v)", line, err)
			}
		}
		quiet := testRunCommand(commandBadWorkflow, "--json", flag, "-q", "-")
		if quiet.Stdout != got.Stdout || quiet.Status != 1 || quiet.Stderr != "" {
			t.Fatalf("quiet suppressed findings or leaked logs: %+v", quiet)
		}
	}
}

func TestCommandJSONHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help", "--", "--json"}, {"--help", "--stdin-filename", "--json"}, {"--help", "workflow.yml", "--json"}} {
		got := testRunCommand("", args...)
		if got.Status != 0 || got.Stdout != "" || !strings.Contains(got.Stderr, "Usage:") {
			t.Fatalf("literal data selected JSON help: %+v", got)
		}
	}
	for _, args := range [][]string{{"--help", "--json"}, {"--json", "-h"}, {"--output=json", "--help"}} {
		got := testRunCommand("", args...)
		var help commandDescription
		if err := json.Unmarshal([]byte(got.Stdout), &help); err != nil {
			t.Fatalf("%+v: %v", got, err)
		}
		if got.Status != 0 || got.Stderr != "" || help.Name != "actionlint" || len(help.Flags) != 18 {
			t.Fatalf("%+v", got)
		}
		for _, flag := range help.Flags {
			if flag.Description == "" || flag.Group == "" || flag.Name == "" {
				t.Errorf("incomplete flag: %+v", flag)
			}
			if flag.Name == "ignore" && !flag.Repeatable {
				t.Error("ignore must be repeatable")
			}
		}
	}
	got := testRunCommand("", "--version", "--json")
	var info commandBuildInfo
	if err := json.Unmarshal([]byte(got.Stdout), &info); err != nil {
		t.Fatal(err)
	}
	if got.Status != 0 || got.Stderr != "" || info != commandBuild() {
		t.Fatalf("%+v", got)
	}
}

func TestCommandConfigAndEmptyArgs(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, dir := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandGoodWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"actionlint"}} {
		var out, errout bytes.Buffer
		cmd := Command{Stdout: &out, Stderr: &errout}
		if code := cmd.Main(args); code != 0 || out.Len() != 0 || errout.Len() != 0 {
			t.Fatalf("empty args imported process args: %d %s %s", code, &out, &errout)
		}
	}
	got := testRunCommand("", "--init-config", "--json")
	var created struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &created); err != nil {
		t.Fatal(err)
	}
	if got.Status != 0 || got.Stderr != "" || filepath.Base(created.Path) != "actionlint.yaml" {
		t.Fatalf("%+v", got)
	}
	data, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "yaml-language-server: $schema=") {
		t.Fatalf("missing editor directive: %s", data)
	}
	refused := testRunCommand("", "--init-config", "--json")
	if refused.Status != 3 || !strings.Contains(refused.Stderr, "already exists") {
		t.Fatalf("%+v", refused)
	}
	if err := os.WriteFile(created.Path, []byte("paths:\n  '**':\n    ignore: ['undefined variable']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand(commandBadWorkflow, "--config-file", created.Path, "--json", "-")
	if got.Status != 0 || got.Stdout != "[]\n" || got.Stderr != "" {
		t.Fatalf("explicit config not applied: %+v", got)
	}
}

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

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

func TestCommandLogRecords(t *testing.T) {
	var out bytes.Buffer
	w := commandJSONLogWriter{out: &out}
	_, _ = io.WriteString(&w, "first ")
	_, _ = io.WriteString(&w, "line\nsecond line\n")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { _, _ = fmt.Fprintf(&w, "worker %d\n", i) })
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 22 {
		t.Fatal(len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
}
