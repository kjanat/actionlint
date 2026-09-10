package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/pflag"
)

func TestHelpColorFlags(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, tc := range []struct {
		name    string
		args    []string
		colored bool
		noColor string
		term    string
	}{
		{name: "redirected root", args: []string{"--help"}},
		{name: "redirected check", args: []string{"check", "--help"}},
		{name: "root force", args: []string{"--color", "--help"}, colored: true},
		{name: "single dash", args: []string{"-color", "-help"}, colored: true},
		{name: "root false", args: []string{"--color=false", "--help"}},
		{name: "no-color first", args: []string{"--no-color", "--color", "--help"}},
		{name: "no-color last", args: []string{"--color", "--no-color", "--help"}},
		{name: "no-color false", args: []string{"--no-color=false", "--color", "--help"}, colored: true},
		{name: "root early help", args: []string{"--help", "--color"}},
		{name: "check always", args: []string{"check", "--color=always", "--help"}, colored: true},
		{name: "check bare color", args: []string{"check", "--color", "--help"}, colored: true},
		{name: "check auto", args: []string{"check", "--color=auto", "--help"}},
		{name: "check never", args: []string{"check", "--color=never", "--help"}},
		{name: "root force then auto", args: []string{"--color", "check", "--color=auto", "--help"}},
		{name: "root force then never", args: []string{"--color", "check", "--color=never", "--help"}},
		{name: "root disable then always", args: []string{"--no-color", "check", "--color=always", "--help"}},
		{name: "help command", args: []string{"--color", "help", "check"}, colored: true},
		{name: "nested command", args: []string{"--color", "config", "show", "--help"}, colored: true},
		{name: "legacy guide", args: []string{"--color", "--help-legacy"}, colored: true},
		{name: "check legacy guide", args: []string{"check", "--color=always", "--help-legacy"}, colored: true},
		{name: "environment disables auto", args: []string{"--help"}, noColor: "1"},
		{name: "force overrides environment", args: []string{"--color", "--help"}, colored: true, noColor: "1"},
		{name: "dumb terminal", args: []string{"--help"}, term: "dumb"},
		{name: "force on dumb terminal", args: []string{"--color", "--help"}, colored: true, term: "dumb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			if tc.term != "" {
				t.Setenv("TERM", tc.term)
			}
			var out, stderr bytes.Buffer
			cmd := Command{Stdout: &out, Stderr: &stderr}
			previous := color.NoColor
			if code := cmd.Main(append([]string{"actionlint"}, tc.args...)); code != 0 || out.Len() != 0 {
				t.Fatalf("help changed streams or exit status: %d, stdout %q, stderr %q", code, &out, &stderr)
			}
			styled := stderr.String()
			if ansi.MatchString(styled) != tc.colored {
				t.Fatalf("colored = %t, want %t: %q", ansi.MatchString(styled), tc.colored, styled)
			}
			stderr.Reset()
			plainArgs := append([]string{"actionlint", "--no-color"}, tc.args...)
			for i, arg := range plainArgs {
				if arg == "--no-color=false" {
					plainArgs[i] = "--no-color"
				}
			}
			if code := cmd.Main(plainArgs); code != 0 {
				t.Fatal(code)
			}
			if plain := ansi.ReplaceAllString(styled, ""); plain != stderr.String() {
				t.Fatalf("styling changed layout:\n%s\nplain:\n%s", plain, &stderr)
			}
			if color.NoColor != previous {
				t.Fatal("help changed process-wide diagnostic color settings")
			}
		})
	}
}

func TestHelpJSONIsNotColored(t *testing.T) {
	for _, args := range [][]string{
		{"--color", "--help", "--json"},
		{"check", "--color=always", "--help", "--json"},
		{"--color", "--json", "config", "show", "--help"},
	} {
		var out, stderr bytes.Buffer
		cmd := Command{Stdout: &out, Stderr: &stderr}
		if code := cmd.Main(append([]string{"actionlint"}, args...)); code != 0 || stderr.Len() != 0 || !json.Valid(out.Bytes()) || strings.ContainsRune(out.String(), '\x1b') {
			t.Fatalf("%q: %d, stdout %q, stderr %q", args, code, &out, &stderr)
		}
	}
}

func TestHelpForcedColorInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "help.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	cmd := Command{Stderr: file}
	if code := cmd.Main([]string{"actionlint", "--color", "--help"}); code != 0 {
		t.Fatal(code)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("\x1b[")) {
		t.Fatalf("forced color lost when redirected: %q, %v", data, err)
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
		if got.Status != 0 || got.Stderr != "" || help.Name != "actionlint" || len(help.Commands) != 6 {
			t.Fatalf("%+v", got)
		}
		for _, flag := range help.Flags {
			if flag.Description == "" || flag.Group == "" || flag.Name == "" {
				t.Errorf("incomplete flag: %+v", flag)
			}
			if flag.Name == "ignore-regex" && !flag.Repeatable {
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

func TestManualNativeJSONContract(t *testing.T) {
	data, err := os.ReadFile("man/actionlint.1.md")
	if err != nil {
		t.Fatal(err)
	}
	result := testRunCommand(commandGoodWorkflow, "check", "--json", "-")
	if result.Status != 0 || !strings.Contains(string(data), strings.TrimSpace(result.Stdout)) {
		t.Fatalf("manual lacks clean JSON example %s", result.Stdout)
	}
}

func TestManualDocumentsEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("man", "actionlint.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n# FLAGS\n")
	if !ok {
		t.Fatal("manual has no FLAGS section")
	}
	body, _, _ = strings.Cut(body, "\n# ")
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`\*\*(--?[^*]+)\*\*`).FindAllStringSubmatch(body, -1) {
		documented[m[1]] = true
	}
	app := newCommandApp(&Command{})
	app.root.Flags().VisitAll(func(f *pflag.Flag) {
		for _, name := range []string{"--" + f.Name, "-" + f.Shorthand} {
			if name == "-" {
				continue
			}
			if !documented[name] {
				t.Errorf("manual is missing %s", name)
			}
			delete(documented, name)
		}
	})
	for name := range documented {
		t.Errorf("manual documents unknown flag %s", name)
	}
}
