package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/internal/processconfig"
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
}

func (e *Execution) Run(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.Cmd, e.Args...)
	cmd.Dir = e.Dir
	cmd.Stderr = nil
	if len(e.Env) > 0 || len(e.UnsetEnv) > 0 {
		cmd.Env = append(cmd.Environ(), e.Env...)
		cmd.Env = slices.DeleteFunc(cmd.Env, func(entry string) bool {
			name, _, _ := strings.Cut(entry, "=")
			return slices.ContainsFunc(e.UnsetEnv, func(unset string) bool {
				return name == unset || runtime.GOOS == "windows" && strings.EqualFold(name, unset)
			})
		})
	}
	// Let os/exec start the reader before copying stdin. Writing the whole script
	// before Start can fill the pipe and deadlock, even with a single worker.
	cmd.Stdin = strings.NewReader(e.Stdin)

	var stdout []byte
	var err error
	if e.CombineOutput {
		stdout, err = cmd.CombinedOutput()
	} else {
		stdout, err = cmd.Output()
	}

	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code := exitErr.ExitCode()

			stderr := exitErr.Stderr
			if e.CombineOutput {
				stderr = stdout
			}

			if code < 0 {
				return nil, fmt.Errorf("%s was terminated. stderr: %q", e.Cmd, stderr)
			}
			if e.RequireSuccess || e.MaxExitCode > 0 && code > e.MaxExitCode {
				return nil, fmt.Errorf("%s exited with status %d. stderr: %q", e.Cmd, code, stderr)
			}

			if len(stdout) == 0 {
				return nil, fmt.Errorf("%s exited with status %d but stdout was empty. stderr: %q", e.Cmd, code, stderr)
			}

			// Providers may use an allowed nonzero status to report findings.
		} else {
			return nil, err
		}
	}

	return stdout, nil
}

// ProcessPool is a manager to run process concurrently. Since running process consumes OS
// resources, running too many processes concurrently causes some issues. On macOS, making too many
// process makes the parent process hang (see issue #3). And running processes which open files can
// cause the error "pipe: too many files to open". To avoid it, this type manages how many processes
// are run at once.
type ProcessPool struct {
	Scope *Scope
	Ctx   context.Context
	sema  *semaphore.Weighted
	wg    *sync.WaitGroup
}

// NewProcessPool creates a new ConcurrentProcess instance. The ctx argument acquires the
// semaphore and runs each child process, so cancelling it kills the processes which are running.
// The `par` argument represents how many processes can be run in parallel. It is recommended to
// use the value returned from runtime.NumCPU() for the argument.
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

// Wait waits all goroutines started by this ProcessPool instance finish.
func (proc *ProcessPool) Wait() {
	proc.wg.Wait() // Wait for all goroutines completing to shutdown
}

// NewCommandRunner creates new external command runner for given executable. The executable path
// is resolved in this function.
func (proc *ProcessPool) NewCommandRunner(exe string, combineOutput bool) (*Command, error) {
	return proc.ConfiguredCommandRunner(exe, nil, combineOutput)
}

func (proc *ProcessPool) ConfiguredCommandRunner(exe string, options *ExternalCommandOptions, combineOutput bool) (*Command, error) {
	p, args, err := ResolveExternalCommandOptions(exe, options)
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

// EmbeddedCommand binds an in-process runner to the same completion owner as
// native commands without performing executable discovery.
func (proc *ProcessPool) EmbeddedCommand(name string, runner func([]string, string) ([]byte, error)) *Command {
	return &Command{proc: proc, Exe: name, Runner: runner}
}

type ExternalCommandOptions = processconfig.Options

func ResolveExternalCommandOptions(command string, options *ExternalCommandOptions) (string, []string, error) {
	return processconfig.ResolveOptions(command, options)
}

func ResolveExternalCommand(command string) (string, []string, error) {
	return processconfig.Resolve(command)
}

// Command is struct to run specific command concurrently with ProcessPool bounding
// number of processes at the same time. This type manages fatal errors while running the command
// by using errgroup.Group. The wait() method must be called at the end for checking if some fatal
// error occurred.
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

// Run runs the command with given arguments and stdin. The callback function is called after the
// process runs. First argument is stdout and the second argument is an error while running the
// process.
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
	exec := &Execution{Cmd: cmd.Exe, Args: args, Stdin: stdin, CombineOutput: cmd.CombineOutput, Env: cmd.Env, UnsetEnv: cmd.UnsetEnv, Dir: dir, MaxExitCode: cmd.MaxExitCode, RequireSuccess: cmd.RequireSuccess}
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
	execution := Execution{Cmd: cmd.Exe, Args: slices.Concat(cmd.LauncherArgs, cmd.Args, args), Stdin: stdin, Dir: cmd.Dir, Env: cmd.Env, UnsetEnv: cmd.UnsetEnv, MaxExitCode: cmd.MaxExitCode, RequireSuccess: cmd.RequireSuccess, CombineOutput: cmd.CombineOutput}
	return execution.Run(ctx)
}
