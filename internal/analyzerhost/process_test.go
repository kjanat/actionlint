package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic" // Note: atomic.Bool was added at Go 1.19
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/execabs"
)

func testStartEchoCommand(t *testing.T, proc *ProcessPool, done *atomic.Bool) {
	t.Helper()

	done.Store(false)
	echo := testSkipIfNoCommand(t, proc, "echo")
	echo.Run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		done.Store(true)
		return nil
	})
	// This function does not wait the command finishes
}

func testSkipIfNoCommand(t *testing.T, p *ProcessPool, cmd string) *Command {
	t.Helper()
	c, err := p.NewCommandRunner(cmd, false)
	if err != nil {
		t.Skipf("%s command is necessary to run this test: %s", cmd, err)
	}
	return c
}

func TestProcessRunConcurrently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test is flaky on Windows")
	}

	p := NewProcessPool(t.Context(), 5)
	sleep := testSkipIfNoCommand(t, p, "sleep")

	start := time.Now()
	for range 5 {
		sleep.Run([]string{"0.1"}, "", func(b []byte, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			return nil
		})
	}
	if err := sleep.Wait(); err != nil {
		t.Fatal(err)
	}
	p.Wait()

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
	p := NewProcessPool(ctx, 1)
	sleep := testSkipIfNoCommand(t, p, "sleep")

	start := time.Now()
	sleep.Run([]string{"30"}, "", func(b []byte, err error) error {
		return err
	})

	// Let the child process start before killing it
	time.Sleep(100 * time.Millisecond)
	cancel()

	err := sleep.Wait()
	p.Wait()

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
	p := NewProcessPool(t.Context(), 1)
	echo, err := p.NewCommandRunner("echo hello", false)
	if err != nil {
		t.Fatalf(`parsing "echo hello" failed: %v`, err)
	}
	echo.Run(nil, "", func(b []byte, err error) error {
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
	p.Wait()

	if !done.Load() {
		t.Error("callback did not run")
	}
}

func TestProcessRunMultipleCommandsConcurrently(t *testing.T) {
	p := NewProcessPool(t.Context(), 3)

	done := make([]bool, 5)
	cmds := make([]*Command, 0, 5)
	for i := range 5 {
		idx := i
		echo := testSkipIfNoCommand(t, p, "echo")
		echo.Run([]string{"hello"}, "", func(b []byte, err error) error {
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
		if err := c.Wait(); err != nil {
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
	p := NewProcessPool(t.Context(), 2)

	done := make([]bool, 3)
	for i := range 3 {
		idx := i
		echo := testSkipIfNoCommand(t, p, "echo")
		echo.Run([]string{"hello"}, "", func(b []byte, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			done[idx] = true
			return nil
		})
	}

	p.Wait()

	for i, b := range done {
		if !b {
			t.Errorf("cmds[%d] did not finish", i)
		}
	}
}

func TestProcessInputStdin(t *testing.T) {
	p := NewProcessPool(t.Context(), 1)
	cat := testSkipIfNoCommand(t, p, "cat")
	out := ""

	cat.Run([]string{}, "this is test", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		out = string(b)
		return nil
	})

	if err := cat.Wait(); err != nil {
		t.Fatal(err)
	}
	p.Wait()

	if out != "this is test" {
		t.Fatalf("stdin was not input to `cat` command: %q", out)
	}
}

func TestProcessStdinHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_STDIN_HELPER") != "1" {
		return
	}
	if expected := os.Getenv("ACTIONLINT_TEST_WORKING_DIR"); expected != "" {
		actual, err := os.Stat(".")
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat(expected)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(actual, want) {
			t.Fatalf("child process did not run in %q", expected)
		}
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestProcessEnvironmentHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_ENVIRONMENT_HELPER") != "1" {
		return
	}
	fmt.Printf("%s\n%s", os.Getenv("PWD"), os.Getenv("LANG"))
	os.Exit(0)
}

func TestProcessWorkingDirectoryEnvironment(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_ENVIRONMENT_HELPER", "1")
	t.Setenv("PWD", parent)
	t.Setenv("LANG", "inherited")
	for _, tc := range []struct {
		name, language, pwd string
		env                 []string
	}{
		{name: "inherited", language: "inherited"},
		{name: "unrelated override", language: "C", env: []string{"LANG=C"}},
		{name: "explicit PWD override", language: "C", pwd: "explicit", env: []string{"LANG=C", "PWD=explicit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			pwd := tc.pwd
			if pwd == "" {
				pwd = directory
				if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
					pwd = parent
				}
			}
			execution := Execution{Cmd: exe, Args: []string{"-test.run=^TestProcessEnvironmentHelper$"}, Dir: directory, Env: tc.env, InheritEnvironment: true}
			output, err := execution.Run(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%s\n%s", pwd, tc.language)
			if string(output) != want {
				t.Fatalf("child directory/environment: got %q, want %q", output, want)
			}
		})
	}
}

func TestProcessScrubbedEnvironment(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANG", "inherited")
	execution := Execution{Cmd: exe, Args: []string{"-test.run=^TestProcessEnvironmentHelper$"}, Env: []string{"ACTIONLINT_TEST_ENVIRONMENT_HELPER=1", "PWD=explicit"}}
	output, err := execution.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "explicit\n" {
		t.Fatalf("scrubbed child saw the host environment: %q", output)
	}
}

func TestProcessExitCodes(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		accepted []int
		status   string
		ok       bool
	}{
		{"listed nonzero", []int{0, 1}, "1", true},
		{"unlisted nonzero", []int{0, 2}, "1", false},
		{"unlisted success", []int{1}, "0", false},
		{"listed success", []int{0}, "0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execution := Execution{Cmd: exe, Args: []string{"-test.run=^TestProcessExitStatusHelper$"}, Env: []string{"ACTIONLINT_TEST_EXIT_STATUS=" + tc.status}, ExitCodes: tc.accepted}
			output, code, err := execution.RunStatus(t.Context())
			if tc.ok != (err == nil) || tc.ok && (string(output) != "reported" || strconv.Itoa(code) != tc.status) {
				t.Fatalf("output=%q code=%d err=%v", output, code, err)
			}
		})
	}
}

func TestProcessExitStatusHelper(t *testing.T) {
	status := os.Getenv("ACTIONLINT_TEST_EXIT_STATUS")
	if status == "" {
		return
	}
	fmt.Print("reported")
	code, err := strconv.Atoi(status)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(code)
}

func TestProcessRelativeExecutableWithWorkingDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	relative := "." + string(filepath.Separator) + filepath.Base(exe)
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprintf("override=%t", override), func(t *testing.T) {
			directory := filepath.Dir(exe)
			if override {
				directory = t.TempDir()
			}
			proc := NewProcessPool(t.Context(), 1)
			cmd, err := proc.ConfiguredCommandRunner("", &ExternalCommandOptions{
				Executable: &relative,
				WorkingDir: filepath.Dir(exe),
				Environment: []string{
					"ACTIONLINT_TEST_STDIN_HELPER=1",
					"ACTIONLINT_TEST_WORKING_DIR=" + directory,
				},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			check := func(output []byte, err error) error {
				if err != nil {
					return err
				}
				if string(output) != "script stdin" {
					return fmt.Errorf("unexpected helper output: %q", output)
				}
				return nil
			}
			args := []string{"-test.run=^TestProcessStdinHelper$"}
			if override {
				cmd.RunInDirectory(args, "script stdin", directory, check)
			} else {
				cmd.Run(args, "script stdin", check)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			proc.Wait()
		})
	}
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
				p := NewProcessPool(ctx, workers)
				cmd := &Command{proc: p, Exe: exe, CombineOutput: combined}
				var completed atomic.Int32
				for range workers {
					cmd.Run([]string{"-test.run=^TestProcessStdinHelper$"}, payload, func(b []byte, err error) error {
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
					err := cmd.Wait()
					p.Wait()
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
	p := NewProcessPool(t.Context(), 1)
	c := &Command{
		proc: p,
		Exe:  "this-command-does-not-exist",
	}

	c.Run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			return fmt.Errorf("yay! error found! %w", err)
		}
		t.Error("command not found error did not occur")
		return nil
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := c.Wait()
	if err == nil || !strings.Contains(err.Error(), "yay! error found!") {
		t.Fatalf("error was not reported by p.Wait(): %v", err)
	}

	p.Wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessErrorInCallback(t *testing.T) {
	p := NewProcessPool(t.Context(), 1)
	echo := testSkipIfNoCommand(t, p, "echo")

	echo.Run([]string{}, "", func(b []byte, err error) error {
		if err != nil {
			t.Error(err)
			return err
		}
		return errors.New("dummy error")
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := echo.Wait()
	if err == nil || err.Error() != "dummy error" {
		t.Fatalf("error was not reported by p.Wait(): %v", err)
	}

	p.Wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessErrorLinterFailed(t *testing.T) {
	p := NewProcessPool(t.Context(), 1)
	ls := testSkipIfNoCommand(t, p, "ls")

	// Running ls with directory which does not exist emulates external liter's failure.
	// For example shellcheck exits with non-zero status but it outputs nothing to stdout when it
	// fails to run.
	ls.Run([]string{"oops-this-directory-does-not-exist"}, "", func(b []byte, err error) error {
		if err != nil {
			return err
		}
		t.Error("error did not occur on running the process")
		return nil
	})

	echoDone := &atomic.Bool{}
	testStartEchoCommand(t, p, echoDone)

	err := ls.Wait()
	if err == nil {
		t.Fatal("error did not occur")
	}
	msg := err.Error()
	if !strings.Contains(msg, "but stdout was empty") || !strings.Contains(msg, "oops-this-directory-does-not-exist") {
		t.Fatalf("Error message was unexpected: %q", msg)
	}

	p.Wait()

	if !echoDone.Load() {
		t.Fatal("a command following the error did not run")
	}
}

func TestProcessRunConcurrentlyAndWait(t *testing.T) {
	p := NewProcessPool(t.Context(), 2)
	echo := testSkipIfNoCommand(t, p, "echo")

	c := make(chan struct{})
	for range 3 {
		go func() {
			for range 5 {
				echo.Run(nil, "", func(b []byte, err error) error {
					return err
				})
			}
			c <- struct{}{}
		}()
	}

	for range 3 {
		<-c
	}

	p.Wait()
}

func TestProcessCombineStdoutAndStderr(t *testing.T) {
	p := NewProcessPool(t.Context(), 1)
	bash := testSkipIfNoCommand(t, p, "bash")
	bash.CombineOutput = true
	script := "echo 'hello stdout'; echo 'hello stderr' >&2"
	done := make(chan string)

	bash.Run([]string{"-c", script}, "", func(b []byte, err error) error {
		if err != nil {
			t.Fatal(err)
			return err
		}
		done <- string(b)
		return nil
	})

	out := <-done
	if err := bash.Wait(); err != nil {
		t.Fatal(err)
	}
	p.Wait()

	if !strings.Contains(out, "hello stdout") {
		t.Errorf("stdout was not captured: %q", out)
	}
	if !strings.Contains(out, "hello stderr") {
		t.Errorf("stderr was not captured: %q", out)
	}
}

func TestProcessCommandExitStatusNonZero(t *testing.T) {
	p := NewProcessPool(t.Context(), 1)
	bash := testSkipIfNoCommand(t, p, "false")
	done := make(chan error)

	bash.Run([]string{}, "", func(b []byte, err error) error {
		done <- err
		return nil
	})

	err := <-done
	if err := bash.Wait(); err != nil {
		t.Fatal(err)
	}
	p.Wait()
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

	p := NewProcessPool(t.Context(), 1)
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			_, err := p.NewCommandRunner(tc.cmd, true)
			if err == nil {
				t.Fatalf("Command %q caused no error", tc)
			}
		})
	}
}

// Backend shutdown must wait for callbacks even when a document scope aborts
// before its normal join, including embedded runners with no native process.
func TestProcessPoolWaitIncludesForkCallbacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := NewProcessPool(t.Context(), 1)
		child := pool.Fork()
		release := make(chan struct{})
		callback := false
		command := &Command{proc: child, Runner: func([]string, string) ([]byte, error) {
			return nil, nil
		}}
		command.Run(nil, "", func([]byte, error) error {
			<-release
			callback = true
			return nil
		})
		waited := false
		go func() { pool.Wait(); waited = true }()
		synctest.Wait()
		if waited {
			t.Fatal("parent finished before child callback")
		}
		close(release)
		synctest.Wait()
		if !waited || !callback {
			t.Fatal("parent did not drain child callbacks")
		}
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	})
}
