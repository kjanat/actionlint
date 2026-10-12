package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"golang.org/x/sys/execabs"
)

func TestTerminalJSONKeepsRedirectedBytes(t *testing.T) {
	data := []byte("{\"unicode\":\"résumé\",\"count\":2}\n")
	var out bytes.Buffer
	if err := writeTerminalJSON(t.Context(), &out, actionlint.ColorOptionKindAlways, data); err != nil {
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
	if err := writeTerminalJSON(t.Context(), file, actionlint.ColorOptionKindAlways, data); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file output changed: %q, %v", got, err)
	}
	if err := writeTerminalJSON(t.Context(), commandFailingIO{}, actionlint.ColorOptionKindAuto, data); err == nil {
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
