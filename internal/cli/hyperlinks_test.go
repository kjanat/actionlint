package cli

import (
	"net/url"
	"os"
	"path/filepath"
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

func TestFileURL(t *testing.T) {
	cases := []struct{ path, want string }{
		{"", ""},
	}
	if filepath.Separator == '\\' {
		cases = append(cases, []struct{ path, want string }{
			{`C:\Users\Kaj Kowalski\répo #1\100%.yaml`, "file:///C:/Users/Kaj%20Kowalski/r%C3%A9po%20%231/100%25.yaml"},
			{"C:/Users/Kaj Kowalski/répo #1/100%.yaml", "file:///C:/Users/Kaj%20Kowalski/r%C3%A9po%20%231/100%25.yaml"},
			{`C:\Users/Kaj Kowalski\répo #1/100%.yaml`, "file:///C:/Users/Kaj%20Kowalski/r%C3%A9po%20%231/100%25.yaml"},
			{`C:\repo\\.github\actionlint.yaml`, "file:///C:/repo/.github/actionlint.yaml"},
			{`\\server\share\folder name\workflow.yml`, "file://server/share/folder%20name/workflow.yml"},
			{`\\?\C:\repo\workflow.yml`, "file:///C:/repo/workflow.yml"},
			{`\\?\UNC\server\share\workflow.yml`, "file://server/share/workflow.yml"},
			{"//server/share/workflow.yml", "file://server/share/workflow.yml"},
			{"//?/C:/repo/workflow.yml", "file:///C:/repo/workflow.yml"},
			{"//?/UNC/server/share/workflow.yml", "file://server/share/workflow.yml"},
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
