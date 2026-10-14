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

func TestHelpUsesTerminalWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("NO_HYPERLINKS", "1")
	counts := make(map[int]int)
	for _, columns := range []int{80, 160} {
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
		slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = slave.Close() })
		if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(columns), Row: 40}); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(&output, master)
			close(done)
		}()
		cmd := Command{Stderr: slave}
		status := cmd.Main([]string{"actionlint", "--help"})
		_ = slave.Close()
		<-done
		if status != 0 {
			t.Fatalf("help status: %d", status)
		}
		text := strings.ReplaceAll(output.String(), "\r", "")
		_, flags, ok := strings.Cut(text, "Input:\n")
		if !ok {
			t.Fatal(text)
		}
		flags, _, _ = strings.Cut(flags, "\nExit status:")
		lines := strings.Split(flags, "\n")
		counts[columns] = len(lines)
		for _, line := range lines {
			if len(line) > columns {
				t.Fatalf("%d-column terminal: line too wide: %s", columns, line)
			}
		}
	}
	if counts[160] >= counts[80] {
		t.Fatalf("wide terminal did not reduce wrapping: %v", counts)
	}
}
