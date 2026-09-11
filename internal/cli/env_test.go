package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestEnvironmentDefaults(t *testing.T) {
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", "")
	t.Setenv("ACTIONLINT_PYFLAKES_BIN", "")
	t.Setenv("ACTIONLINT_OUTPUT_FORMAT", "json")
	t.Setenv("ACTIONLINT_STDIN_FILENAME", "from-env.yml")
	t.Setenv("ACTIONLINT_SUMMARY", "true")
	for _, args := range [][]string{{"-"}, {"check", "-"}} {
		got := testRunCommand(commandBadWorkflow, args...)
		if got.Status != 1 || !json.Valid([]byte(got.Stdout)) || !strings.Contains(got.Stdout, "from-env.yml") || !strings.Contains(got.Stderr, `"summary"`) {
			t.Fatalf("%v: %+v", args, got)
		}
	}
	for _, args := range [][]string{
		{"--output-format=text", "--summary=false", "--stdin-filename=explicit.yml", "-"},
		{"check", "-", "--output-format=text", "--summary=false", "--stdin-filename=explicit.yml"},
		{"--summary=false", "--stdin-filename=explicit.yml", "-format", "{{range .}}{{.Filepath}}: {{.Message}}{{end}}", "-"},
	} {
		got := testRunCommand(commandBadWorkflow, args...)
		if got.Status != 1 || json.Valid([]byte(got.Stdout)) || !strings.Contains(got.Stdout, "explicit.yml") || got.Stderr != "" {
			t.Fatalf("flags did not override environment: %v: %+v", args, got)
		}
	}
}

func TestEnvironmentConfigSelection(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("env.yaml", []byte("policy:\n  require-job-timeout: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("explicit.yaml", []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_CONFIG", "env.yaml")
	for _, args := range [][]string{{"-"}, {"check", "-"}} {
		got := testRunCommand(commandGoodWorkflow, args...)
		if got.Status != 1 || !strings.Contains(got.Stdout, "require-job-timeout") {
			t.Fatalf("selected config not applied: %v: %+v", args, got)
		}
	}
	if got := testRunCommand("", "config", "path"); got.Stdout != "env.yaml\n" || got.Status != 0 {
		t.Fatalf("config path: %+v", got)
	}
	t.Setenv("ACTIONLINT_NO_CONFIG", "true")
	for _, args := range [][]string{{"--config=explicit.yaml", "-"}, {"check", "--no-config", "-"}} {
		if got := testRunCommand(commandGoodWorkflow, args...); got.Status != 0 || got.Stdout+got.Stderr != "" {
			t.Fatalf("explicit selection did not override environment: %v: %+v", args, got)
		}
	}
}

func TestEnvironmentFiltersAndLogging(t *testing.T) {
	// Use the same message filter with both parsers; explicit filters replace it.
	t.Setenv("ACTIONLINT_IGNORE_REGEX", `["missing"]`)
	for _, args := range [][]string{{"-"}, {"check", "-"}} {
		if got := testRunCommand(commandBadWorkflow, args...); got.Status != 0 || got.Stdout+got.Stderr != "" {
			t.Fatalf("environment filter: %+v", got)
		}
	}
	if got := testRunCommand(commandBadWorkflow, "check", "--ignore-regex=never-matches", "-"); got.Status != 1 {
		t.Fatalf("explicit filter should replace environment filter: %+v", got)
	}
	t.Setenv("ACTIONLINT_LOG_LEVEL", "info")
	if got := testRunCommand(commandGoodWorkflow, "-"); got.Status != 0 || !strings.Contains(got.Stderr, "verbose:") {
		t.Fatalf("environment log level: %+v", got)
	}
	if got := testRunCommand(commandGoodWorkflow, "-verbose=false", "-"); got.Status != 0 || got.Stderr != "" {
		t.Fatalf("explicit false must override log level: %+v", got)
	}
}

func TestEnvironmentValidationByOperation(t *testing.T) {
	t.Setenv("ACTIONLINT_CONFIG", "does-not-exist.yml")
	t.Setenv("ACTIONLINT_IGNORE_REGEX", "not JSON")
	t.Setenv("ACTIONLINT_LOG_LEVEL", "invalid")
	t.Setenv("ACTIONLINT_SHELLCHECK_FLAGS", `"unterminated`)
	for _, args := range [][]string{{"--help"}, {"version", "--json"}, {"--version", "--json"}, {"completion", "fish"}, {"rules"}} {
		if got := testRunCommand("", args...); got.Status != 0 {
			t.Fatalf("unrelated environment blocked %v: %+v", args, got)
		}
	}
	t.Setenv("ACTIONLINT_LOG_LEVEL", "")
	if got := testRunCommand(commandGoodWorkflow, "check", "--no-config", "-"); got.Status != 2 || !strings.Contains(got.Stderr, "ACTIONLINT_IGNORE_REGEX") {
		t.Fatalf("invalid relevant environment should fail: %+v", got)
	}
	if got := testRunCommand(commandGoodWorkflow, "check", "--no-config", "--ignore-regex=none", "-"); got.Status != 0 {
		t.Fatalf("overridden invalid environment should not be parsed: %+v", got)
	}
}

func TestEnvironmentWordsAndChildEnvironment(t *testing.T) {
	t.Setenv("ACTIONLINT_TEST_FORWARD", "inherited")
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{`-e SC2086 --name 'two words'`, []string{"-e", "SC2086", "--name", "two words"}},
		{`["C:\\Program Files\\tool", "", "$VALUE", "$(command)"]`, []string{`C:\Program Files\tool`, "", "$VALUE", "$(command)"}},
	} {
		got, err := parseEnvironmentWords(tc.value)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("words %q: %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	for _, value := range []string{`[1]`, `[null]`, `"unterminated`, `ok; other`, `["\u0000"]`} {
		if _, err := parseEnvironmentWords(value); err == nil {
			t.Fatalf("accepted invalid arguments %q", value)
		}
	}
	for _, value := range []string{
		`ACTIONLINT_TEST_FORWARD EMPTY= NAME='value with spaces=here'`,
		`{"ACTIONLINT_TEST_FORWARD":null,"EMPTY":"","NAME":"value with spaces=here"}`,
	} {
		got, err := parseChildEnvironment(value)
		want := []string{"ACTIONLINT_TEST_FORWARD=inherited", "EMPTY=", "NAME=value with spaces=here"}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("environment %q: %q, %v; want %q", value, got, err, want)
		}
	}
	for _, value := range []string{`{"BAD=NAME":"secret"}`, `{"NAME":12}`, `9NAME=secret`, `{"NAME":"\u0000"}`} {
		if _, err := parseChildEnvironment(value); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid environment accepted or value leaked: %v", err)
		}
	}
}

func TestEnvironmentRejectsNullFilters(t *testing.T) {
	t.Setenv("ACTIONLINT_IGNORE_REGEX", `[null]`)
	got := testRunCommand(commandBadWorkflow, "check", "--no-config", "-")
	if got.Status != 2 || !strings.Contains(got.Stderr, "ACTIONLINT_IGNORE_REGEX") {
		t.Fatalf("null must not become an empty regex that suppresses every finding: %+v", got)
	}
}

func TestEnvironmentDisabledTools(t *testing.T) {
	for _, tool := range []string{"SHELLCHECK", "PYFLAKES"} {
		t.Setenv("ACTIONLINT_"+tool+"_BIN", "")
		t.Setenv("ACTIONLINT_"+tool+"_FLAGS", `"unterminated`)
		t.Setenv("ACTIONLINT_"+tool+"_ENV", `{"INVALID":123}`)
	}
	var out, stderr bytes.Buffer
	cmd := Command{Stdout: &out, Stderr: &stderr}
	if status := cmd.Main([]string{"actionlint", "doctor", "--json", "--no-config"}); status != 0 || strings.Count(out.String(), `"status":"disabled"`) != 2 {
		t.Fatalf("empty executable must disable the tool without parsing its settings: exit %d: %s %s", status, &out, &stderr)
	}
}

type environmentToolReport struct {
	Args  []string
	Input string
	Value string
}

func environmentToolHelper() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1
	}
	report := environmentToolReport{os.Args[1:], string(input), os.Getenv("ACTIONLINT_TEST_CHILD")}
	data, err := json.Marshal(report)
	if err != nil {
		return 1
	}
	if err := os.WriteFile(os.Getenv("ACTIONLINT_TEST_REPORT"), data, 0o600); err != nil {
		return 1
	}
	if report.Value == "shell" {
		fmt.Print(`{"comments":[]}`)
	}
	return 0
}

func TestEnvironmentExternalTools(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "tool with spaces")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(tool, data, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_CHILD", "parent")
	reports := map[string]string{}
	for _, toolName := range []string{"SHELLCHECK", "PYFLAKES"} {
		kind := "shell"
		if toolName == "PYFLAKES" {
			kind = "python"
		}
		path := filepath.Join(t.TempDir(), "report.json")
		reports[kind] = path
		env, err := json.Marshal(map[string]string{"ACTIONLINT_TEST_TOOL_PROCESS": "1", "ACTIONLINT_TEST_REPORT": path, "ACTIONLINT_TEST_CHILD": kind})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("ACTIONLINT_"+toolName+"_BIN", tool)
		t.Setenv("ACTIONLINT_"+toolName+"_FLAGS", `["argument with spaces", "", "$LITERAL"]`)
		t.Setenv("ACTIONLINT_"+toolName+"_ENV", string(env))
	}
	input := commandGoodWorkflow + "      - shell: python\n        run: print('ok')\n"
	for _, args := range [][]string{{"-"}, {"check", "-"}} {
		var out, stderr bytes.Buffer
		cmd := Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &stderr}
		status := cmd.Main(append([]string{"actionlint", "--no-config", "--no-color"}, args...))
		if status != 0 || out.Len()+stderr.Len() != 0 {
			t.Fatalf("exit %d: %s %s", status, &out, &stderr)
		}
		for kind, path := range reports {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var report environmentToolReport
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.Value != kind || report.Input == "" || len(report.Args) < 3 || !reflect.DeepEqual(report.Args[:3], []string{"argument with spaces", "", "$LITERAL"}) {
				t.Fatalf("%s did not receive its settings: %+v", kind, report)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if os.Getenv("ACTIONLINT_TEST_CHILD") != "parent" {
		t.Fatal("child overrides changed parent environment")
	}
	var out, stderr bytes.Buffer
	cmd := Command{Stdout: &out, Stderr: &stderr}
	if status := cmd.Main([]string{"actionlint", "doctor", "--json", "--no-config"}); status != 0 {
		t.Fatalf("doctor exit %d: %s", status, &stderr)
	}
	var doctor struct {
		Tools []doctorTool `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &doctor); err != nil {
		t.Fatal(err)
	}
	for _, tool := range doctor.Tools {
		if tool.Status != "available" || !reflect.DeepEqual(tool.Arguments, []string{"argument with spaces", "", "$LITERAL"}) {
			t.Fatalf("doctor does not describe configured tool: %+v", tool)
		}
	}
	if strings.Contains(out.String(), "ACTIONLINT_TEST_CHILD") {
		t.Fatal("doctor exposed environment overrides")
	}
	for _, path := range reports {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("doctor executed a tool")
		}
	}
	got := testRunCommand("", "doctor", "--json", "--no-config")
	if got.Status != 0 || !strings.Contains(got.Stdout, `"status":"disabled"`) {
		t.Fatalf("explicit empty tool override: %+v", got)
	}
}
