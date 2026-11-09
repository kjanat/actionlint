package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("ACTIONLINT_GIT_COMMAND_HELPER"); mode != "" {
		switch mode {
		case "orphan":
			executable, err := os.Executable()
			if err != nil {
				os.Exit(2)
			}
			child := exec.CommandContext(context.Background(), executable)
			child.Env = append(os.Environ(), "ACTIONLINT_GIT_COMMAND_HELPER=pipe-holder")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stdout, child.Process.Pid)
		case "pipe-holder":
			time.Sleep(3 * time.Second)
		case "stall":
			fmt.Fprint(os.Stdout, "ready")
			time.Sleep(time.Minute)
		case "success":
			fmt.Fprint(os.Stdout, "output")
			fmt.Fprint(os.Stderr, "notice")
		default:
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommandBoundsInheritedPipes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_GIT_COMMAND_HELPER", "orphan")
	output, err := Command(t.Context(), executable, t.TempDir()).Output()
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("inherited stdout pipe returned %v, want exec.ErrWaitDelay", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("helper did not report its child: %q: %v", output, err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := child.Release(); err != nil {
			t.Errorf("release pipe holder: %v", err)
		}
	}()
	if err := child.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("stop pipe holder: %v", err)
	}
}

func TestQueryBoundsStalledCommands(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"caller deadline", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ACTIONLINT_GIT_COMMAND_HELPER", "stall")
			ctx := t.Context()
			want := context.DeadlineExceeded
			limit := 3 * time.Second
			switch mode {
			case "caller deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			case "caller cancellation":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				timer := time.AfterFunc(time.Second, cancel)
				defer timer.Stop()
				want, limit = context.Canceled, 3*time.Second
			}
			start := time.Now()
			output, err := Query(ctx, executable, t.TempDir(), nil)
			if !errors.Is(err, want) || string(output.Stdout) != "ready" {
				t.Fatalf("stalled query: stdout=%q error=%v, want a started process and %v", output.Stdout, err, want)
			}
			if elapsed := time.Since(start); elapsed > limit {
				t.Fatalf("query took %s, want at most %s", elapsed, limit)
			}
		})
	}
}

func TestReadIndexBoundsStalledGitWithoutCallerDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, name), content, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ACTIONLINT_GIT_COMMAND_HELPER", "stall")
	start := time.Now()
	_, err = ReadIndex(t.Context(), t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "read Git index") {
		t.Fatalf("stalled Git index query returned %v, want its deadline and operation", err)
	}
	if elapsed := time.Since(start); elapsed > queryTimeout+3*time.Second {
		t.Fatalf("index query took %s, want its local query deadline", elapsed)
	}
	if err := t.Context().Err(); err != nil {
		t.Fatalf("index query canceled its caller: %v", err)
	}
}

func TestQueryRejectsCanceledContextBeforeStarting(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_GIT_COMMAND_HELPER", "success")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	output, err := Query(ctx, executable, t.TempDir(), nil)
	if !errors.Is(err, context.Canceled) || len(output.Stdout) != 0 || len(output.Stderr) != 0 {
		t.Fatalf("canceled query ran: output=%+v error=%v", output, err)
	}
}

func TestQueryPreservesSeparateOutputStreams(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_GIT_COMMAND_HELPER", "success")
	output, err := Query(t.Context(), executable, t.TempDir(), nil)
	if err != nil || string(output.Stdout) != "output" || string(output.Stderr) != "notice" {
		t.Fatalf("successful query: output=%+v error=%v", output, err)
	}
}
