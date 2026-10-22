package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionlint "actionlint.kjanat.dev/internal/engine"
	"golang.org/x/sys/execabs"
)

func TestTerminalJSONKeepsRedirectedBytes(t *testing.T) {
	data := []byte("{\"unicode\":\"résumé\",\"count\":2}\n")
	var out bytes.Buffer
	if err := writePrettyJSON(t.Context(), &out, actionlint.ColorOptionKindAlways, data, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("buffer output changed: %q", &out)
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := writePrettyJSON(t.Context(), file, actionlint.ColorOptionKindAlways, data, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file output changed: %q, %v", got, err)
	}
	if err := writePrettyJSON(t.Context(), commandFailingIO{}, actionlint.ColorOptionKindAuto, data, false); err == nil {
		t.Fatal("lost output error")
	}
}

func TestTerminalJSONJQ(t *testing.T) {
	jq, err := execabs.LookPath("jq")
	if err != nil {
		t.Skip("jq not installed")
	}
	data := []byte(`{"name":"actionlint","list":[true,"résumé"]}`)
	plain, err := formatJSONWithJQ(t.Context(), jq, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(plain) || bytes.Count(plain, []byte("\n")) < 4 || bytes.ContainsRune(plain, '\x1b') {
		t.Fatalf("jq did not pretty-print plain JSON: %q", plain)
	}
	t.Setenv("NO_COLOR", "1")
	colored, err := formatJSONWithJQ(t.Context(), jq, data, true)
	if err != nil || !bytes.Contains(colored, []byte("\x1b[")) {
		t.Fatalf("explicit color not honored: %q, %v", colored, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := formatJSONWithJQ(ctx, jq, data, false); err == nil {
		t.Fatal("jq ignored cancellation")
	}
}

func TestJSONEnvironmentAndOverride(t *testing.T) {
	t.Setenv("ACTIONLINT_JSON", "true")
	t.Setenv("ACTIONLINT_JSON_PRETTY", "false")
	for _, args := range [][]string{{"version"}, {"--version"}, {"doctor", "--no-config"}, {"--help"}} {
		got := testRunCommand("", args...)
		if got.Status != 0 || !json.Valid([]byte(got.Stdout)) || strings.Count(got.Stdout, "\n") != 1 || got.Stderr != "" {
			t.Fatalf("JSON environment %v: %+v", args, got)
		}
	}
	if got := testRunCommand("", "version", "--json=false"); got.Status != 0 || json.Valid([]byte(got.Stdout)) {
		t.Fatalf("explicit false did not override JSON environment: %+v", got)
	}
}

func TestExplicitJSONPrettySelectsJSON(t *testing.T) {
	for _, tc := range []struct {
		name             string
		args, equivalent []string
	}{
		{"rules", []string{"rules", "--json-pretty"}, []string{"rules", "--json"}},
		{"root prefix", []string{"--json-pretty", "rules"}, []string{"rules", "--json"}},
		{"root check", []string{"--json-pretty", "-"}, []string{"--json", "-"}},
		{"check", []string{"check", "--json-pretty", "-"}, []string{"check", "--json", "-"}},
		{"help", []string{"--help", "--json-pretty"}, []string{"--help", "--json"}},
		{"command help", []string{"rules", "--help", "--json-pretty"}, []string{"rules", "--help", "--json"}},
		{"parse failure", []string{"rules", "--unknown", "--json-pretty"}, []string{"rules", "--unknown", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := testRunCommand(commandGoodWorkflow, tc.args...)
			want := testRunCommand(commandGoodWorkflow, tc.equivalent...)
			if got.Status != want.Status || compactJSONForComparison(got.Stdout) != compactJSONForComparison(want.Stdout) || compactJSONForComparison(got.Stderr) != compactJSONForComparison(want.Stderr) || !json.Valid([]byte(got.Stdout+got.Stderr)) {
				t.Fatalf("explicit pretty flag did not select JSON: %+v, want %+v", got, want)
			}
		})
	}
	for _, args := range [][]string{
		{"rules"},
		{"rules", "--json-pretty=false"},
		{"rules", "--json-pretty", "--json=false"},
	} {
		got := testRunCommand("", args...)
		if got.Status != 0 || json.Valid([]byte(got.Stdout)) {
			t.Fatalf("unexpected JSON format: %v: %+v", args, got)
		}
	}
	for _, format := range []string{"text", "oneline", "sarif", "jsonl"} {
		args := []string{"check", "--output-format=" + format, "-"}
		got := testRunCommand(commandGoodWorkflow, append([]string{"--json-pretty"}, args...)...)
		want := testRunCommand(commandGoodWorkflow, args...)
		if got.Status != want.Status || got.Stderr != want.Stderr || compactJSONForComparison(got.Stdout) != compactJSONForComparison(want.Stdout) {
			t.Fatalf("pretty flag changed explicit %s format: %+v, want %+v", format, got, want)
		}
	}
}

func TestExplicitJSONPrettyOverridesEnvironmentFormat(t *testing.T) {
	t.Setenv("ACTIONLINT_OUTPUT_FORMAT", "oneline")
	t.Setenv("ACTIONLINT_TEMPLATE", "{{range .}}{{.Kind}}{{end}}")
	got := testRunCommand(commandGoodWorkflow, "check", "--json-pretty", "-")
	if got.Status != 0 || got.Stderr != "" || !json.Valid([]byte(got.Stdout)) {
		t.Fatalf("environment overrode the explicit JSON selection: %+v", got)
	}
}

func TestExplicitPrettyJSONWithoutJQ(t *testing.T) {
	t.Setenv("PATH", "")
	for _, args := range [][]string{
		{"rules", "--json-pretty"},
		{"--help", "--json-pretty"},
		{"check", "--help", "--json-pretty"},
		{"--no-config", "--json-pretty", "-"},
		{"check", "--no-config", "--output-format=sarif", "--json-pretty", "-"},
	} {
		got := testRunCommand(commandGoodWorkflow, append([]string{"--color"}, args...)...)
		if got.Status != 0 || got.Stderr != "" || !json.Valid([]byte(got.Stdout)) || !strings.Contains(got.Stdout, "\n  ") || strings.ContainsRune(got.Stdout, '\x1b') {
			t.Fatalf("redirected JSON was not indented without jq: %v: %+v", args, got)
		}
	}
	got := testRunCommand("", "rules", "--json", "--json-pretty=false")
	if got.Status != 0 || strings.Count(got.Stdout, "\n") != 1 || !json.Valid([]byte(got.Stdout)) {
		t.Fatalf("explicit false did not keep compact JSON: %+v", got)
	}
	t.Chdir(t.TempDir())
	got = testRunCommand(commandGoodWorkflow, "check", "--no-config", "--json-pretty", "--output-file=report.json", "-")
	data, err := os.ReadFile("report.json")
	if got.Status != 0 || got.Stdout != "" || got.Stderr != "" || err != nil || !json.Valid(data) || !bytes.Contains(data, []byte("\n  ")) || bytes.ContainsRune(data, '\x1b') {
		t.Fatalf("report file was not indented: %+v, %q, %v", got, data, err)
	}
}

func TestPresentationEnvironment(t *testing.T) {
	t.Setenv("ACTIONLINT_COLOR", "always")
	t.Setenv("ACTIONLINT_HYPERLINKS", "always")
	t.Setenv("NO_COLOR", "")
	t.Setenv("NO_HYPERLINKS", "")
	for _, tc := range []struct {
		args         []string
		no           string
		color, links bool
	}{
		{args: []string{"--help"}, color: true, links: true},
		{args: []string{"check", "--help"}, color: true, links: true},
		{args: []string{"--help"}, no: "1"},
		{args: []string{"--color", "--hyperlinks=always", "--help"}, no: "1", color: true, links: true},
		{args: []string{"check", "--color=never", "--hyperlinks=never", "--help"}},
	} {
		t.Setenv("NO_COLOR", tc.no)
		t.Setenv("NO_HYPERLINKS", tc.no)
		var out, stderr bytes.Buffer
		cmd := Command{Stdout: &out, Stderr: &stderr}
		status := cmd.Main(append([]string{"actionlint"}, tc.args...))
		if status != 0 || out.Len() != 0 || strings.Contains(stderr.String(), "\x1b[") != tc.color || strings.Contains(stderr.String(), "\x1b]8;") != tc.links {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", tc.args, status, &out, &stderr)
		}
	}
}

func compactJSONForComparison(s string) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(s)); err != nil {
		return s
	}
	return compact.String()
}
