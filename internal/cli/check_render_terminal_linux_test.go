//go:build linux

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCheckAutomaticColorFollowsTerminal(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("FORCE_HYPERLINKS", "")
	for _, tc := range []struct {
		noColor, term string
		wantColor     bool
	}{
		{term: "xterm-256color", wantColor: true},
		{noColor: "1", term: "xterm-256color"},
		{term: "dumb"},
	} {
		t.Setenv("NO_COLOR", tc.noColor)
		t.Setenv("TERM", tc.term)
		for _, modern := range []bool{false, true} {
			name := fmt.Sprintf("NO_COLOR=%q TERM=%q modern=%t", tc.noColor, tc.term, modern)
			args := []string{"actionlint", "--summary=false", "--no-tool=shellcheck", "--no-tool=ruff", "--hyperlinks=never"}
			if modern {
				args = append(args, "check", "--no-config")
			}
			terminal, finish := checkTestTerminal(t)
			cmd := Command{Stdin: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"), Stdout: terminal, Stderr: io.Discard}
			code := cmd.MainContext(t.Context(), append(args, "-"))
			text := finish()
			if code != 1 || !strings.Contains(text, "missing") {
				t.Fatalf("%s: exit=%d output=%q", name, code, text)
			}
			if strings.Contains(text, "\x1b[") != tc.wantColor {
				t.Fatalf("%s: color=%t, want %t: %q", name, !tc.wantColor, tc.wantColor, text)
			}
		}
	}
}

func checkTestTerminal(t *testing.T) (*os.File, func() string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Close() })
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, master)
		close(done)
	}()
	return terminal, func() string {
		_ = terminal.Close()
		<-done
		return output.String()
	}
}
