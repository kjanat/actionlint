package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestHelpHyperlinks(t *testing.T) {
	t.Setenv("NO_HYPERLINKS", "")
	t.Setenv("FORCE_HYPERLINKS", "")
	t.Setenv("NO_COLOR", "1")
	osc := regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)
	for _, tc := range []struct {
		name      string
		args      []string
		no, force string
		want      bool
	}{
		{"redirected", []string{"--help"}, "", "", false},
		{"root always", []string{"--hyperlinks=always", "--help"}, "1", "", true},
		{"root single dash", []string{"-hyperlinks", "always", "-help"}, "", "", true},
		{"root early help", []string{"--help", "--hyperlinks=always"}, "", "", false},
		{"modern always", []string{"check", "--help", "--hyperlinks=always"}, "", "", true},
		{"nested command", []string{"config", "show", "--hyperlinks=always", "--help"}, "", "", true},
		{"help command", []string{"--hyperlinks=always", "help", "check"}, "", "", true},
		{"completion help", []string{"completion", "--hyperlinks=always", "--help"}, "", "", true},
		{"legacy help", []string{"--hyperlinks=always", "--help-legacy"}, "", "", true},
		{"modern legacy help", []string{"check", "--hyperlinks=always", "--help-legacy"}, "", "", true},
		{"environment force", []string{"--help"}, "", "1", true},
		{"environment disable", []string{"--help"}, "1", "1", false},
		{"never overrides force", []string{"check", "--hyperlinks=never", "--help"}, "", "1", false},
		{"auto overrides prefix", []string{"--hyperlinks=always", "check", "--hyperlinks=auto", "--help"}, "1", "", false},
		{"no-color is independent", []string{"--no-color", "--hyperlinks=always", "--help"}, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_HYPERLINKS", tc.no)
			t.Setenv("FORCE_HYPERLINKS", tc.force)
			got := testRunCommand("", tc.args...)
			if got.Status != 0 || got.Stdout != "" || strings.Contains(got.Stderr, "\x1b]8;;") != tc.want {
				t.Fatalf("unexpected help output: %+v", got)
			}
			plain := osc.ReplaceAllString(got.Stderr, "")
			compact := strings.Contains(plain, "\nFlags:\n")
			if strings.Contains(plain, "\x1b") || (compact && !strings.Contains(plain, "Docs: https://actionlint.kjanat.dev\n")) || (!compact && (!strings.Contains(plain, "Project:\n  "+"https://github.com/kjanat/actionlint"+"\n") || !strings.Contains(plain, "/docs/usage.md\n"))) {
				t.Fatalf("links lost readable destinations or leaked escapes: %q", plain)
			}
			if compact && tc.want && !strings.Contains(got.Stderr, "\x1b]8;;https://actionlint.kjanat.dev\x1b\\") {
				t.Fatal("missing documentation hyperlink")
			}
			if tc.want && !strings.Contains(got.Stderr, "\x1b]8;;"+"https://github.com/kjanat/actionlint"+"\x1b\\") {
				t.Fatal("missing project hyperlink")
			}
		})
	}
}

func TestHyperlinksDoNotChangeMachineOutput(t *testing.T) {
	t.Setenv("FORCE_HYPERLINKS", "1")
	for _, args := range [][]string{
		{"--help", "--json"}, {"check", "--help", "--json"}, {"config", "show", "--help", "--json"},
		{"--version"}, {"version", "--json"}, {"doctor", "--json"}, {"completion", "powershell"},
		{"check", "--output-format=json", "-"}, {"check", "--output-format=jsonl", "-"},
		{"check", "--output-format=sarif", "-"}, {"check", "--output-format=github", "-"},
		{"-format", "{{json .}}", "-"}, {"-"},
	} {
		got := testRunCommand(commandBadWorkflow, append([]string{"--hyperlinks=always"}, args...)...)
		want := testRunCommand(commandBadWorkflow, append([]string{"--hyperlinks=never"}, args...)...)
		if got != want || strings.Contains(got.Stdout+got.Stderr, "\x1b]8;") {
			t.Fatalf("hyperlinks changed %q: %+v / %+v", args, got, want)
		}
		if strings.Contains(strings.Join(args, " "), "--help --json") && !json.Valid([]byte(got.Stdout)) {
			t.Fatalf("invalid JSON help: %s", got.Stdout)
		}
	}
}

func TestInvalidHyperlinkMode(t *testing.T) {
	for _, args := range [][]string{
		{"--hyperlinks=bad", "--help"}, {"check", "--hyperlinks=bad", "--help"},
		{"config", "show", "--hyperlinks=bad"}, {"--hyperlinks=", "--version"},
	} {
		got := testRunCommand("", args...)
		if got.Status != 2 || got.Stdout != "" || !strings.Contains(got.Stderr, "invalid hyperlink mode") {
			t.Fatalf("invalid mode accepted: %q: %+v", args, got)
		}
	}
}

func TestHyperlinksInRedirectedHelp(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "help.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	cmd := cli.Command{Stderr: file}
	if code := cmd.Main([]string{"actionlint", "--color", "--hyperlinks=always", "--help"}); code != 0 {
		t.Fatal(code)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Contains(data, []byte("\x1b]8;;")) || !bytes.Contains(data, []byte("\x1b[")) {
		t.Fatalf("lost hyperlink or color escapes: %q, %v", data, err)
	}
}

func TestHyperlinkCompletion(t *testing.T) {
	for _, args := range [][]string{
		{"__complete", "--hyperlinks", ""},
		{"__complete", "check", "--hyperlinks", ""},
		{"__complete", "config", "show", "--hyperlinks", ""},
	} {
		var out, stderr bytes.Buffer
		cmd := cli.Command{Stdout: &out, Stderr: &stderr}
		if code := cmd.Main(append([]string{"actionlint"}, args...)); code != 0 || !strings.Contains(out.String(), "auto\nalways\nnever\n:4") {
			t.Fatalf("missing hyperlink choices: %d, %s, %s", code, &out, &stderr)
		}
	}
}
