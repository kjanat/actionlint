package hostprocess

import (
	"path/filepath"

	"github.com/mattn/go-shellwords"
	"golang.org/x/sys/execabs"
)

// CommandOptions with a non-nil Executable replaces the command line with a
// literal executable path, and an empty Executable disables the tool. A nil
// Executable parses the configured command line.
type CommandOptions struct {
	// IgnoreConfig marks an explicit application invocation that overrides the
	// repository's command settings, even when it names the default executable.
	IgnoreConfig bool
	// AllowWasm permits a configured Wasm backend despite a native executable override.
	AllowWasm bool
	// Optional lets a default command that cannot run be skipped with a warning.
	// Explicitly configured checkers should leave this false.
	Optional   bool
	Executable *string
	Arguments  []string
	// LauncherArguments precede engine arguments without parsing.
	LauncherArguments []string
	Environment       []string
	// WorkingDir sets the default child directory and resolves explicit relative
	// executable paths. Bare command names are resolved through PATH.
	WorkingDir string
}

func CommandEnabled(command string, options *CommandOptions) bool {
	if options != nil && options.Executable != nil {
		return *options.Executable != ""
	}
	return command != ""
}

func ResolveCommandOptions(command string, options *CommandOptions) (string, []string, error) {
	if options == nil {
		return ResolveExecutable(command)
	}
	if !CommandEnabled(command, options) {
		return "", nil, nil
	}
	if options.Executable != nil {
		executable := *options.Executable
		if options.WorkingDir != "" && !filepath.IsAbs(executable) && filepath.Base(executable) != executable {
			executable = filepath.Join(options.WorkingDir, executable)
		}
		path, err := execabs.LookPath(executable)
		return path, append([]string(nil), options.Arguments...), err
	}
	path, args, err := ResolveExecutable(command)
	return path, append(args, options.Arguments...), err
}

func ResolveExecutable(exe string) (string, []string, error) {
	c, err := execabs.LookPath(exe)
	if err == nil {
		return c, nil, nil
	}

	if a, err := shellwords.Parse(exe); err == nil && len(a) > 0 {
		if c, err := execabs.LookPath(a[0]); err == nil {
			return c, a[1:], nil
		}
	}

	return "", nil, err
}
