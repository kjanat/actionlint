package process

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

	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// Execution represents a single command line execution.
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
	// minus UnsetEnv. Otherwise the child sees Env alone: tool providers run
	// scrubbed and name every variable they pass through.
	InheritEnvironment bool
	// ExitCodes, when not empty, are the only statuses accepted, and a nonzero
	// one only with nonempty stdout. They replace MaxExitCode and RequireSuccess.
	ExitCodes []int
	// OutputLimit bounds combined stdout/stderr bytes while the child runs.
	// Zero preserves the caller's existing capture behavior.
	OutputLimit int64
}

func (e *Execution) Run(ctx context.Context) ([]byte, error) {
	stdout, _, err := e.RunStatus(ctx)
	return stdout, err
}

// RunStatus runs the execution and also returns the accepted exit status.
func (e *Execution) RunStatus(ctx context.Context) ([]byte, int, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	// The child command and the status checks work on detached copies, so the
	// caller's slices are never shared with os/exec or the filters below.
	args, unsetEnv, exitCodes := slices.Clone(e.Args), slices.Clone(e.UnsetEnv), slices.Clone(e.ExitCodes)
	cmd := exec.CommandContext(runContext, e.Cmd, args...)
	cmd.Dir = e.Dir
	cmd.Stderr = nil
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
	// Let os/exec start the reader before copying stdin. Writing the whole script
	// before Start can fill the pipe and deadlock, even with a single worker.
	cmd.Stdin = strings.NewReader(e.Stdin)

	var stdout []byte
	var capturedStderr []byte
	var err error
	switch {
	case e.OutputLimit > 0:
		stdout, capturedStderr, err = boundedCommandOutput(cmd, e.OutputLimit, e.CombineOutput, cancel)
	case e.CombineOutput:
		stdout, err = cmd.CombinedOutput()
	default:
		stdout, err = cmd.Output()
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

			// Providers may use an allowed nonzero status to report findings.
			return stdout, code, nil
		}
		return nil, 0, err
	}
	if len(exitCodes) > 0 && !slices.Contains(exitCodes, 0) {
		return nil, 0, fmt.Errorf("%s exited with status 0, which the tool does not report", e.Cmd)
	}
	return stdout, 0, nil
}

// ProcessPool limits concurrent processes to avoid exhausting operating system
// resources. Too many processes can hang the parent on macOS (issue #3) or
// exhaust the available file descriptors.
type ProcessPool struct {
	Scope *Scope
	Ctx   context.Context
	sema  *semaphore.Weighted
	wg    *sync.WaitGroup
}

// NewProcessPool creates a pool that runs up to par processes at once. The
// context controls semaphore acquisition and child processes; cancelling it
// stops running children. runtime.NumCPU() is a recommended value for par.
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

// Wait waits for all goroutines started by the pool to finish.
func (proc *ProcessPool) Wait() {
	proc.wg.Wait()
}

// NewCommandRunner creates a command runner and resolves the executable path.
func (proc *ProcessPool) NewCommandRunner(exe string, combineOutput bool) (*Command, error) {
	return proc.ConfiguredCommandRunner(exe, nil, combineOutput)
}

func (proc *ProcessPool) ConfiguredCommandRunner(exe string, options *processconfig.Options, combineOutput bool) (*Command, error) {
	p, args, err := processconfig.ResolveOptions(exe, options)
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

// EmbeddedCommand creates an in-process runner that the pool waits for alongside
// native commands. It does not look up an executable.
func (proc *ProcessPool) EmbeddedCommand(name string, runner func([]string, string) ([]byte, error)) *Command {
	return &Command{proc: proc, Exe: name, Runner: runner}
}

// Command runs a specific command through a ProcessPool. Call Wait to wait for
// its runs and check for errors returned by their callbacks.
type Command struct {
	// runner replaces process execution for embedded checkers. It must honor
	// the same arguments/stdout contract; callbacks still join through eg.
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

// Run starts the command with the given arguments and stdin. After it finishes,
// Run calls callback with stdout and any execution error.
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

// Wait waits until all goroutines for this command finish. Note that it does not wait for
// goroutines for other commands.
func (cmd *Command) Wait() error {
	return cmd.eg.Wait()
}

// Fork shares process capacity while giving each document its own join boundary.
func (proc *ProcessPool) Fork() *ProcessPool {
	return &ProcessPool{Ctx: proc.Ctx, sema: proc.sema, wg: proc.wg, Scope: &Scope{}}
}

func (proc *ProcessPool) Finish() error {
	if proc.Scope == nil {
		return nil
	}
	return proc.Scope.Finish()
}

// Execute runs one command synchronously under the pool's shared process limit.
// Both synchronous backends and callback adapters use the same resource budget.
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

// Invoke runs execution synchronously under the pool's shared process limit.
// The caller assembles the complete argument vector and environment; the
// command contributes its executable, or its embedded runner, which reports
// status 0. Unlike Execute, nothing of the command's own arguments,
// environment or directory is added.
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
