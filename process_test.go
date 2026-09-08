package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic" // Note: atomic.Bool was added at Go 1.19
	"testing"
	"time"

	"golang.org/x/sys/execabs"
)

func testStartEchoCommand(t *testing.T, proc *concurrentProcess, done *atomic.Bool) {
	t.Helper()

	done.Store(false)
	echo := testSkipIfNoCommand(t, proc, "echo")
	echo.run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		done.Store(true)
		return nil
	})
	// This function does not wait the command finishes
}

func testSkipIfNoCommand(t *testing.T, p *concurrentProcess, cmd string) *externalCommand {
	t.Helper()
	c, err := p.newCommandRunner(cmd, false)
	if err != nil {
		t.Skipf("%s command is necessary to run this test: %s", cmd, err)
	}
	return c
}

func TestProcessRunConcurrently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test is flaky on Windows")
	}

	p := newConcurrentProcess(t.Context(), 5)
	sleep := testSkipIfNoCommand(t, p, "sleep")

	start := time.Now()
	for range 5 {
		sleep.run([]string{"0.1"}, "", func(b []byte, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			return nil
		})
	}
	if err := sleep.wait(); err != nil {
		t.Fatal(err)
	}
	p.wait()

	sec := time.Since(start).Seconds()
	if sec >= 0.5 {
		t.Fatalf("commands did not run concurrently. running five `sleep 0.1` commands took %v seconds", sec)
	}
}

func TestProcessCancelStopsRunningCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test is flaky on Windows")
	}

	ctx, cancel := context.WithCancel(t.Context())
	p := newConcurrentProcess(ctx, 1)
	sleep := testSkipIfNoCommand(t, p, "sleep")

	start := time.Now()
	sleep.run([]string{"30"}, "", func(b []byte, err error) error {
		return err
	})

	// Let the child process start before killing it
	time.Sleep(100 * time.Millisecond)
	cancel()

	err := sleep.wait()
	p.wait()

	if err == nil {
		t.Fatal("cancelling the context did not stop `sleep 30`")
	}
	if !strings.Contains(err.Error(), "was terminated") {
		t.Fatalf("`sleep 30` should have been killed by the cancellation but the error was: %v", err)
	}
	if sec := time.Since(start).Seconds(); sec >= 5 {
		t.Fatalf("`sleep 30` kept running after the cancellation. waiting for it took %v seconds", sec)
	}
}

func TestProcessRunWithArgs(t *testing.T) {
	if _, err := execabs.LookPath("echo"); err != nil {
		t.Skipf("echo command is necessary to run this test: %s", err)
	}

	var done atomic.Bool
	p := newConcurrentProcess(t.Context(), 1)
	echo, err := p.newCommandRunner("echo hello", false)
	if err != nil {
		t.Fatalf(`parsing "echo hello" failed: %v`, err)
	}
	echo.run(nil, "", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		if string(b) != "hello\n" {
			t.Errorf("unexpected output: %q", b)
		}
		done.Store(true)
		return nil
	})
	p.wait()

	if !done.Load() {
		t.Error("callback did not run")
	}
}

func TestProcessRunMultipleCommandsConcurrently(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 3)

	done := make([]bool, 5)
	cmds := make([]*externalCommand, 0, 5)
	for i := range 5 {
		idx := i
		echo := testSkipIfNoCommand(t, p, "echo")
		echo.run([]string{"hello"}, "", func(b []byte, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			done[idx] = true
			return nil
		})
		cmds = append(cmds, echo)
	}

	for i, c := range cmds {
		if err := c.wait(); err != nil {
			t.Errorf("cmds[%d] failed: %s", i, err)
		}
	}

	for i, b := range done {
		if !b {
			t.Errorf("cmds[%d] did not finish", i)
		}
	}
}

func TestProcessWaitMultipleCommandsFinish(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 2)

	done := make([]bool, 3)
	for i := range 3 {
		idx := i
		echo := testSkipIfNoCommand(t, p, "echo")
		echo.run([]string{"hello"}, "", func(b []byte, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			done[idx] = true
			return nil
		})
	}

	p.wait()

	for i, b := range done {
		if !b {
			t.Errorf("cmds[%d] did not finish", i)
		}
	}
}

func TestProcessInputStdin(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	cat := testSkipIfNoCommand(t, p, "cat")
	out := ""

	cat.run([]string{}, "this is test", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		out = string(b)
		return nil
	})

	if err := cat.wait(); err != nil {
		t.Fatal(err)
	}
	p.wait()

	if out != "this is test" {
		t.Fatalf("stdin was not input to `cat` command: %q", out)
	}
}

func TestProcessStdinHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_STDIN_HELPER") != "1" {
		return
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

// The Linux reproducer in rhysd/actionlint#651 filled a 64 KiB pipe with one script.
// Larger input must finish with one worker as well as concurrent workers.
func TestProcessConcurrentStdinDoesNotDeadlock(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_STDIN_HELPER", "1")
	payload := strings.Repeat("0123456789abcdef", 8192)

	for _, workers := range []int{1, 5} {
		for _, combined := range []bool{false, true} {
			t.Run(fmt.Sprintf("workers=%d/combined=%t", workers, combined), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				p := newConcurrentProcess(ctx, workers)
				cmd := &externalCommand{proc: p, exe: exe, combineOutput: combined}
				var completed atomic.Int32
				for range workers {
					cmd.run([]string{"-test.run=^TestProcessStdinHelper$"}, payload, func(b []byte, err error) error {
						if err != nil {
							return err
						}
						if string(b) != payload {
							return fmt.Errorf("stdin was not preserved: got %d bytes, want %d", len(b), len(payload))
						}
						completed.Add(1)
						return nil
					})
				}
				done := make(chan error, 1)
				go func() {
					err := cmd.wait()
					p.wait()
					done <- err
				}()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("stdin larger than the pipe buffer did not finish")
				}
				if got := completed.Load(); got != int32(workers) {
					t.Fatalf("completed %d commands, want %d", got, workers)
				}
			})
		}
	}
}

func TestProcessErrorCommandNotFound(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	c := &externalCommand{
		proc: p,
		exe:  "this-command-does-not-exist",
	}

	c.run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			return fmt.Errorf("yay! error found! %w", err)
		}
		t.Error("command not found error did not occur")
		return nil
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := c.wait()
	if err == nil || !strings.Contains(err.Error(), "yay! error found!") {
		t.Fatalf("error was not reported by p.Wait(): %v", err)
	}

	p.wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessErrorInCallback(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	echo := testSkipIfNoCommand(t, p, "echo")

	echo.run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		return errors.New("dummy error")
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := echo.wait()
	if err == nil || err.Error() != "dummy error" {
		t.Fatalf("error was not reported by p.Wait(): %v", err)
	}

	p.wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessErrorLinterFailed(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	ls := testSkipIfNoCommand(t, p, "ls")

	// Running ls with directory which does not exist emulates external liter's failure.
	// For example shellcheck exits with non-zero status but it outputs nothing to stdout when it
	// fails to run.
	ls.run([]string{"oops-this-directory-does-not-exist"}, "", func(b []byte, err error) error {
		if err != nil {
			return err
		}
		t.Error("error did not occur on running the process")
		return nil
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := ls.wait()
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "but stdout was empty") || !strings.Contains(msg, "oops-this-directory-does-not-exist") {
		t.Fatalf("Error message was unexpected: %q", msg)
	}

	p.wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessRunConcurrentlyAndWait(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 2)
	echo := testSkipIfNoCommand(t, p, "echo")

	c := make(chan struct{})
	for range 3 {
		go func() {
			for range 5 {
				echo.run(nil, "", func(b []byte, err error) error {
					return err
				})
			}
			c <- struct{}{}
		}()
	}

	for range 3 {
		<-c
	}

	p.wait()
}

func TestProcessCombineStdoutAndStderr(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	bash := testSkipIfNoCommand(t, p, "bash")
	bash.combineOutput = true
	script := "echo 'hello stdout'; echo 'hello stderr' >&2"
	done := make(chan string)

	bash.run([]string{"-c", script}, "", func(b []byte, err error) error {
		if err != nil {
			t.Fatal(err)
			return err
		}
		done <- string(b)
		return nil
	})

	out := <-done
	if err := bash.wait(); err != nil {
		t.Fatal(err)
	}
	p.wait()

	if !strings.Contains(out, "hello stdout") {
		t.Errorf("stdout was not captured: %q", out)
	}
	if !strings.Contains(out, "hello stderr") {
		t.Errorf("stderr was not captured: %q", out)
	}
}

func TestProcessCommandExitStatusNonZero(t *testing.T) {
	p := newConcurrentProcess(t.Context(), 1)
	bash := testSkipIfNoCommand(t, p, "false")
	done := make(chan error)

	bash.run([]string{}, "", func(b []byte, err error) error {
		done <- err
		return nil
	})

	err := <-done
	if err := bash.wait(); err != nil {
		t.Fatal(err)
	}
	p.wait()
	if err == nil {
		t.Fatal("Error did not happen")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exited with status 1") {
		t.Fatalf("Unexpected error happened: %q", msg)
	}
}

func TestProcessCommandlineParseError(t *testing.T) {
	tests := []struct {
		what string
		cmd  string
	}{
		{
			what: "broken command line",
			cmd:  "'broken' 'arg",
		},
		{
			what: "executable file not found",
			cmd:  "this-command-does-not-exist",
		},
		{
			what: "empty",
			cmd:  "",
		},
	}

	p := newConcurrentProcess(t.Context(), 1)
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			_, err := p.newCommandRunner(tc.cmd, true)
			if err == nil {
				t.Fatalf("Command %q caused no error", tc)
			}
		})
	}
}
