package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-isatty"
)

const projectURL = "https://github.com/kjanat/actionlint"

type hyperlinkMode string

func (m *hyperlinkMode) String() string {
	if *m == "" {
		return "auto"
	}
	return string(*m)
}

func (*hyperlinkMode) Type() string { return "mode" }

func (m *hyperlinkMode) Set(value string) error {
	switch value {
	case "auto", "always", "never":
		*m = hyperlinkMode(value)
		return nil
	default:
		return fmt.Errorf("invalid hyperlink mode %q: choose auto, always or never", value)
	}
}

func (m *hyperlinkMode) enabled(terminal bool, env func(string) string) bool {
	switch *m {
	case "always":
		return true
	case "never":
		return false
	}
	if env("NO_HYPERLINKS") != "" {
		return false
	}
	if env("FORCE_HYPERLINKS") != "" {
		return true
	}
	return terminal && terminalHyperlinks(env)
}

// Terminal hints are documented by https://github.com/chalk/supports-hyperlinks.
// Multiplexers may strip OSC 8 even when the outer terminal supports it.
func terminalHyperlinks(env func(string) string) bool {
	term := env("TERM")
	if term == "dumb" || env("TMUX") != "" || env("STY") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") {
		return false
	}
	if env("WT_SESSION") != "" {
		return true
	}
	switch env("TERM_PROGRAM") {
	case "WezTerm", "ghostty", "zed":
		return true
	case "iTerm.app":
		return terminalVersionAtLeast(env("TERM_PROGRAM_VERSION"), 3, 1)
	case "vscode":
		return terminalVersionAtLeast(env("TERM_PROGRAM_VERSION"), 1, 72)
	}
	switch term {
	case "xterm-kitty", "alacritty":
		return true
	}
	vte := env("VTE_VERSION")
	if strings.Contains(vte, ".") {
		return vte != "0.50.0" && terminalVersionAtLeast(vte, 0, 50)
	}
	version, err := strconv.Atoi(vte)
	return err == nil && version >= 5001
}

func terminalVersionAtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	x, xErr := strconv.Atoi(parts[0])
	y, yErr := strconv.Atoi(parts[1])
	return xErr == nil && yErr == nil && (x > major || x == major && y >= minor)
}

func terminalFile(out io.Writer) (*os.File, bool) {
	f, ok := out.(*os.File)
	return f, ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()))
}

func (a *commandApp) helpHyperlinks() bool {
	_, terminal := terminalFile(a.streams.Stderr)
	return !a.jsonOutput() && a.inv.Render.Hyperlinks.enabled(terminal, os.Getenv)
}

func terminalLink(enabled bool, label, url string) string {
	if !enabled || url == "" {
		return label
	}
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func fileLink(enabled bool, path string) string {
	if path == "" {
		return ""
	}
	if strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return strconv.Quote(path)
	}
	label := filepath.Clean(path)
	if !enabled {
		return label
	}
	return terminalLink(true, label, fileURL(path))
}

// File URI syntax follows https://www.rfc-editor.org/rfc/rfc8089.
func fileURL(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	path = filepath.ToSlash(abs)
	u := url.URL{Scheme: "file"}
	if filepath.Separator == '\\' {
		switch {
		case strings.HasPrefix(strings.ToUpper(path), "//?/UNC/"):
			path = "//" + path[len("//?/UNC/"):]
		case strings.HasPrefix(path, "//?/"):
			path = path[len("//?/"):]
		case strings.HasPrefix(path, "//./"):
			return ""
		}
		switch {
		case strings.HasPrefix(path, "//"):
			var rest string
			u.Host, rest, _ = strings.Cut(path[2:], "/")
			path = "/" + rest
		case len(path) >= 2 && path[1] == ':':
			path = "/" + path
		default:
			return ""
		}
	}
	u.Path = path
	return u.String()
}
