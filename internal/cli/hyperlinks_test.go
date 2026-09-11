package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHyperlinkPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		mode                    hyperlinkMode
		no, force               string
		terminal, capable, want bool
	}{
		{name: "detected", terminal: true, capable: true, want: true},
		{name: "unknown terminal", terminal: true},
		{name: "redirected", capable: true},
		{name: "disabled", no: "1", terminal: true, capable: true},
		{name: "zero disables too", no: "0", terminal: true, capable: true},
		{name: "force on redirected stream", force: "1", want: true},
		{name: "zero forces too", force: "0", want: true},
		{name: "disable beats force", no: "1", force: "1", terminal: true, capable: true},
		{name: "always beats disable", mode: "always", no: "1", want: true},
		{name: "never beats force", mode: "never", force: "1", terminal: true, capable: true},
		{name: "auto uses environment", mode: "auto", force: "1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"NO_HYPERLINKS": tc.no, "FORCE_HYPERLINKS": tc.force, "TERM": "xterm-256color"}
			if tc.capable {
				env["WT_SESSION"] = "session"
			}
			if got := tc.mode.enabled(tc.terminal, func(name string) string { return env[name] }); got != tc.want {
				t.Fatalf("enabled = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestHyperlinkTerminalDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"windows terminal", map[string]string{"WT_SESSION": "session"}, true},
		{"zed", map[string]string{"TERM_PROGRAM": "zed"}, true},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, true},
		{"alacritty", map[string]string{"TERM": "alacritty"}, true},
		{"iterm", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.1.0"}, true},
		{"old iterm", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.0.0"}, false},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.72.0"}, true},
		{"old vscode", map[string]string{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.71.0"}, false},
		{"unknown vscode version", map[string]string{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "unknown"}, false},
		{"vte", map[string]string{"VTE_VERSION": "5001"}, true},
		{"dotted vte", map[string]string{"VTE_VERSION": "0.50.1"}, true},
		{"broken vte", map[string]string{"VTE_VERSION": "0.50.0"}, false},
		{"old vte", map[string]string{"VTE_VERSION": "4900"}, false},
		{"unknown terminal", map[string]string{"TERM": "xterm-256color"}, false},
		{"dumb", map[string]string{"TERM": "dumb", "WT_SESSION": "session"}, false},
		{"tmux", map[string]string{"TERM_PROGRAM": "WezTerm", "TMUX": "session"}, false},
		{"screen", map[string]string{"TERM": "screen-256color", "WT_SESSION": "session"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalHyperlinks(func(name string) string { return tc.env[name] }); got != tc.want {
				t.Fatalf("supported = %t, want %t", got, tc.want)
			}
		})
	}
}

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
			if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "Project:\n  "+projectURL+"\n") || !strings.Contains(plain, "/docs/usage.md\n") {
				t.Fatalf("links lost readable destinations or leaked escapes: %q", plain)
			}
			if tc.want && !strings.Contains(got.Stderr, "\x1b]8;;"+projectURL+"\x1b\\") {
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
	cmd := Command{Stderr: file}
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
		cmd := Command{Stdout: &out, Stderr: &stderr}
		if code := cmd.Main(append([]string{"actionlint"}, args...)); code != 0 || !strings.Contains(out.String(), "auto\nalways\nnever\n:4") {
			t.Fatalf("missing hyperlink choices: %d, %s, %s", code, &out, &stderr)
		}
	}
}

func TestFileURL(t *testing.T) {
	cases := []struct{ path, want string }{
		{"", ""},
	}
	if filepath.Separator == '\\' {
		cases = append(cases, []struct{ path, want string }{
			{`C:\Users\Kaj Kowalski\répo #1\100%.yaml`, "file:///C:/Users/Kaj%20Kowalski/r%C3%A9po%20%231/100%25.yaml"},
			{`C:\repo\\.github\actionlint.yaml`, "file:///C:/repo/.github/actionlint.yaml"},
			{`\\server\share\folder name\workflow.yml`, "file://server/share/folder%20name/workflow.yml"},
			{`\\?\C:\repo\workflow.yml`, "file:///C:/repo/workflow.yml"},
			{`\\?\UNC\server\share\workflow.yml`, "file://server/share/workflow.yml"},
			{`\\.\NUL`, ""},
		}...)
	} else {
		cases = append(cases, []struct{ path, want string }{
			{"/tmp/répo #1/100%.yaml", "file:///tmp/r%C3%A9po%20%231/100%25.yaml"},
			{"/tmp/repo//.github/actionlint.yaml", "file:///tmp/repo/.github/actionlint.yaml"},
			{"/tmp/a?b\\c.yaml", "file:///tmp/a%3Fb%5Cc.yaml"},
		}...)
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := fileURL(tc.path); got != tc.want {
				t.Fatalf("URL = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("relative path", func(t *testing.T) {
		t.Chdir(t.TempDir())
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		link, err := url.Parse(fileURL("config #%.yaml"))
		if err != nil || link.Scheme != "file" || link.Host != "" || link.Fragment != "" || link.RawQuery != "" {
			t.Fatalf("invalid file URI: %v (%v)", link, err)
		}
		path := link.Path
		if filepath.Separator == '\\' {
			path = strings.TrimPrefix(path, "/")
		}
		if got, want := filepath.FromSlash(path), filepath.Join(cwd, "config #%.yaml"); got != want {
			t.Fatalf("link resolves to %q, want %q", got, want)
		}
	})
}

func TestFileLinkPresentation(t *testing.T) {
	path := "." + string(filepath.Separator) + string(filepath.Separator) + "config.yaml"
	if got := fileLink(false, path); got != "config.yaml" {
		t.Fatalf("unclean path label: %q", got)
	}
	if got := fileLink(true, ""); got != "" {
		t.Fatalf("empty path produced a link: %q", got)
	}
	if got := fileLink(true, "bad\tname.yaml"); got != `"bad\tname.yaml"` {
		t.Fatalf("control character leaked into terminal output: %q", got)
	}
}
