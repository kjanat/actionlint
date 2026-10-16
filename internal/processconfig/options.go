// Package processconfig resolves native tool invocations without running analysis.
package processconfig

import (
	"path/filepath"

	"github.com/mattn/go-shellwords"
	"golang.org/x/sys/execabs"
)

// Options supplies literal arguments and child environment overrides.
// A non-nil Executable replaces the command line with a literal executable path;
// an empty executable disables the tool. Nil retains the existing command parser.
type Options struct {
	// IgnoreConfig marks an explicit application invocation that overrides the
	// repository's command settings, even when it names the default executable.
	IgnoreConfig bool
	// Optional permits automatic Ruff discovery to skip an unavailable executable.
	// Explicitly configured checkers should leave this false.
	Optional   bool
	Executable *string
	Arguments  []string
	// LauncherArguments precede engine arguments without parsing.
	LauncherArguments []string
	Environment       []string
	// WorkingDir sets the default child directory and resolves explicit relative
	// executable paths. Bare command names retain PATH lookup.
	WorkingDir string
}

func Enabled(command string, options *Options) bool {
	if options != nil && options.Executable != nil {
		return *options.Executable != ""
	}
	return command != ""
}

// ResolveOptions locates a configured tool without executing it.
func ResolveOptions(command string, options *Options) (string, []string, error) {
	if options == nil {
		return Resolve(command)
	}
	if !Enabled(command, options) {
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
	path, args, err := Resolve(command)
	return path, append(args, options.Arguments...), err
}

// Resolve locates an executable or parses a command with arguments without running it.
func Resolve(exe string) (string, []string, error) {
	c, err := execabs.LookPath(exe)
	if err == nil {
		return c, nil, nil
	}

	// Try to parse the string as a command line instead of a single executable file path.
	if a, err := shellwords.Parse(exe); err == nil && len(a) > 0 {
		if c, err := execabs.LookPath(a[0]); err == nil {
			return c, a[1:], nil
		}
	}

	return "", nil, err
}
