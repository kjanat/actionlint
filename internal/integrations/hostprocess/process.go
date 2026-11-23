package hostprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type Execution struct {
	MaxExitCode    int
	RequireSuccess bool
	Cmd            string
	Args           []string
	Stdin          string
	CombineOutput  bool
	Env            []string
	UnsetEnv       []string
	Dir            string
	// InheritEnvironment starts the child with the host environment plus Env,
	// minus UnsetEnv. Otherwise the child sees Env alone.
	InheritEnvironment bool
	// They replace MaxExitCode and RequireSuccess.
	ExitCodes []int
	// OutputLimit bounds combined stdout/stderr bytes while the child runs.
	OutputLimit   int64
	busyRetryWait func(context.Context, time.Duration) error
}

func (e *Execution) Run(ctx context.Context) ([]byte, error) {
	stdout, _, err := e.RunStatus(ctx)
	return stdout, err
}

func (e *Execution) RunStatus(ctx context.Context) ([]byte, int, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	args, unsetEnv, exitCodes := slices.Clone(e.Args), slices.Clone(e.UnsetEnv), slices.Clone(e.ExitCodes)

	var stdout []byte
	var capturedStderr []byte
	var err error
	for delay := 5 * time.Millisecond; ; delay *= 2 {
		cmd := e.command(runContext, args, unsetEnv)
		switch {
		case e.OutputLimit > 0:
			stdout, capturedStderr, err = boundedCommandOutput(cmd, e.OutputLimit, e.CombineOutput, cancel)
		case e.CombineOutput:
			stdout, err = cmd.CombinedOutput()
		default:
			stdout, err = cmd.Output()
		}
		// A concurrent fork can briefly retain a write descriptor for a
		// freshly published executable until the child reaches exec.
		if cmd.Process != nil || !executableBusy(err) || delay > 160*time.Millisecond {
			break
		}
		wait := waitForBusyRetry
		if e.busyRetryWait != nil {
			wait = e.busyRetryWait
		}
		if err := wait(runContext, delay); err != nil {
			return nil, 0, err
		}
	}

	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code := exitErr.ExitCode()

			stderr := exitErr.Stderr
			if e.OutputLimit > 0 {
				stderr = capturedStderr
			}
			if e.CombineOutput {
				stderr = stdout
			}

			if code < 0 {
				return nil, code, fmt.Errorf("%s was terminated. stderr: %q", e.Cmd, stderr)
			}
			accepted := !e.RequireSuccess && (e.MaxExitCode <= 0 || code <= e.MaxExitCode)
			if len(exitCodes) > 0 {
				accepted = slices.Contains(exitCodes, code)
			}
			if !accepted {
				return nil, code, fmt.Errorf("%s exited with status %d. stderr: %q", e.Cmd, code, stderr)
			}

			if len(stdout) == 0 {
				return nil, code, fmt.Errorf("%s exited with status %d but stdout was empty. stderr: %q", e.Cmd, code, stderr)
			}

			return stdout, code, nil
		}
		return nil, 0, err
	}
	if len(exitCodes) > 0 && !slices.Contains(exitCodes, 0) {
		return nil, 0, fmt.Errorf("%s exited with status 0, which the tool does not report", e.Cmd)
	}
	return stdout, 0, nil
}

func waitForBusyRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
}

func (e *Execution) command(ctx context.Context, args, unsetEnv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, e.Cmd, args...)
	cmd.Dir = e.Dir
	switch {
	case !e.InheritEnvironment:
		// A non-nil empty slice keeps os/exec from inheriting the host environment.
		cmd.Env = append([]string{}, e.Env...)
	case len(e.Env) > 0 || len(unsetEnv) > 0:
		cmd.Env = append(cmd.Environ(), e.Env...)
	}
	if len(unsetEnv) > 0 && cmd.Env != nil {
		cmd.Env = slices.DeleteFunc(cmd.Env, func(entry string) bool {
			name, _, _ := strings.Cut(entry, "=")
			return slices.ContainsFunc(unsetEnv, func(unset string) bool {
				return name == unset || runtime.GOOS == "windows" && strings.EqualFold(name, unset)
			})
		})
	}
	// Let os/exec start the reader before copying stdin.
	cmd.Stdin = strings.NewReader(e.Stdin)
	return cmd
}

type ProcessPool struct {
	Scope *Scope
	Ctx   context.Context
	sema  *semaphore.Weighted
	wg    *sync.WaitGroup
}

// NewProcessPool creates a pool that runs up to par processes at once. The
// context controls semaphore acquisition and child processes; cancelling it
// stops running children.
func NewProcessPool(ctx context.Context, par int) *ProcessPool {
	return &ProcessPool{
		Ctx:  ctx,
		sema: semaphore.NewWeighted(int64(par)),
		wg:   &sync.WaitGroup{},
	}
}

func (proc *ProcessPool) Run(eg *errgroup.Group, exec *Execution, callback func([]byte, error) error) {
	proc.wg.Add(1)
	eg.Go(func() error {
		defer proc.wg.Done()
		if err := proc.sema.Acquire(proc.Ctx, 1); err != nil {
			return fmt.Errorf("could not acquire semaphore to run %q: %w", exec.Cmd, err)
		}
		stdout, err := exec.Run(proc.Ctx)
		proc.sema.Release(1)
		return callback(stdout, err)
	})
}

func (proc *ProcessPool) Wait() {
	proc.wg.Wait()
}

func (proc *ProcessPool) NewCommandRunner(exe string, combineOutput bool) (*Command, error) {
	return proc.ConfiguredCommandRunner(exe, nil, combineOutput)
}

func (proc *ProcessPool) ConfiguredCommandRunner(exe string, options *CommandOptions, combineOutput bool) (*Command, error) {
	p, args, err := ResolveCommandOptions(exe, options)
	if err != nil {
		return nil, err
	}
	// Resolve before per-script working directories change relative executable paths.
	if p != "" {
		p, err = filepath.Abs(p)
		if err != nil {
			return nil, err
		}
	}
	cmd := &Command{
		proc:          proc,
		Exe:           p,
		Args:          args,
		CombineOutput: combineOutput,
	}
	if options != nil {
		cmd.LauncherArgs = slices.Clone(options.LauncherArguments)
		cmd.Env = append([]string(nil), options.Environment...)
		cmd.Dir = options.WorkingDir
	}
	return cmd, nil
}

func (proc *ProcessPool) EmbeddedCommand(name string, runner func([]string, string) ([]byte, error)) *Command {
	return &Command{proc: proc, Exe: name, Runner: runner}
}

// Command runs a specific command through a ProcessPool. Call Wait to wait for
// its runs and check for errors returned by their callbacks.
type Command struct {
	Runner         func(args []string, stdin string) ([]byte, error)
	MaxExitCode    int
	RequireSuccess bool
	proc           *ProcessPool
	eg             errgroup.Group
	Exe            string
	Args           []string
	LauncherArgs   []string
	CombineOutput  bool
	Env            []string
	UnsetEnv       []string
	Dir            string
}

func (cmd *Command) Run(args []string, stdin string, callback func([]byte, error) error) {
	cmd.RunInDirectory(args, stdin, cmd.Dir, callback)
}

func (cmd *Command) RunInDirectory(args []string, stdin, dir string, callback func([]byte, error) error) {
	if len(cmd.Args) > 0 {
		allArgs := make([]string, 0, len(cmd.Args)+len(args))
		allArgs = append(allArgs, cmd.Args...)
		allArgs = append(allArgs, args...)
		args = allArgs
	}
	args = append(slices.Clone(cmd.LauncherArgs), args...)
	if cmd.Runner != nil {
		cmd.proc.wg.Add(1)
		cmd.eg.Go(func() error {
			defer cmd.proc.wg.Done()
			stdout, err := cmd.Runner(args, stdin)
			return callback(stdout, err)
		})
		return
	}
	exec := &Execution{Cmd: cmd.Exe, Args: args, Stdin: stdin, CombineOutput: cmd.CombineOutput, Env: cmd.Env, UnsetEnv: cmd.UnsetEnv, Dir: dir, MaxExitCode: cmd.MaxExitCode, RequireSuccess: cmd.RequireSuccess, InheritEnvironment: true}
	cmd.proc.Run(&cmd.eg, exec, callback)
}

func (cmd *Command) Wait() error {
	return cmd.eg.Wait()
}

func (proc *ProcessPool) Fork() *ProcessPool {
	return &ProcessPool{Ctx: proc.Ctx, sema: proc.sema, wg: proc.wg, Scope: &Scope{}}
}

func (proc *ProcessPool) Finish() error {
	if proc.Scope == nil {
		return nil
	}
	return proc.Scope.Finish()
}

func (cmd *Command) Execute(ctx context.Context, args []string, stdin string) ([]byte, error) {
	cmd.proc.wg.Add(1)
	defer cmd.proc.wg.Done()
	if err := cmd.proc.sema.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer cmd.proc.sema.Release(1)
	execution := Execution{Cmd: cmd.Exe, Args: slices.Concat(cmd.LauncherArgs, cmd.Args, args), Stdin: stdin, Dir: cmd.Dir, Env: cmd.Env, UnsetEnv: cmd.UnsetEnv, MaxExitCode: cmd.MaxExitCode, RequireSuccess: cmd.RequireSuccess, CombineOutput: cmd.CombineOutput, InheritEnvironment: true}
	return execution.Run(ctx)
}

func (cmd *Command) Invoke(ctx context.Context, execution Execution) ([]byte, int, error) {
	cmd.proc.wg.Add(1)
	defer cmd.proc.wg.Done()
	if err := cmd.proc.sema.Acquire(ctx, 1); err != nil {
		return nil, 0, err
	}
	defer cmd.proc.sema.Release(1)
	if cmd.Runner != nil {
		// The runner may keep its output and errors; callers own what Invoke
		// returns.
		stdout, err := cmd.Runner(execution.Args, execution.Stdin)
		if err != nil {
			return nil, 0, errors.New(err.Error())
		}
		return bytes.Clone(stdout), 0, nil
	}
	execution.Cmd = cmd.Exe
	return execution.RunStatus(ctx)
}
