package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/internal/workspace"

	"actionlint.kjanat.dev/internal/repoconfig"

	"actionlint.kjanat.dev/internal/render"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

type ColorOptionKind = render.Color

const (
	ColorOptionKindAuto   = render.ColorAuto
	ColorOptionKindAlways = render.ColorAlways
	ColorOptionKindNever  = render.ColorNever
)

// LinterOptions is set of options for Linter instance. This struct is used for NewLinter factory
// function call. The zero value LinterOptions{} represents the default behavior.
type LinterOptions struct {
	// Verbose is flag if verbose log output is enabled.
	Verbose bool
	// Debug is flag if debug log output is enabled.
	Debug bool
	// LogWriter is io.Writer object to use to print log outputs. Note that error outputs detected
	// by the linter are not included in the log outputs.
	LogWriter io.Writer
	// Color is option for colorizing error outputs. See ColorOptionKind document for each enum values.
	Color ColorOptionKind
	// Oneline is flag if one line output is enabled. When enabling it, one error is output per one
	// line. It is useful when reading outputs from programs.
	Oneline bool
	// Shellcheck is executable for running shellcheck external command. It can be command name like
	// "shellcheck" or file path like "/path/to/shellcheck", "path/to/shellcheck". When this value
	// is empty, shellcheck won't run to check scripts in workflow file.
	Shellcheck string
	// Deprecated: Pyflakes integration was removed. This field is ignored.
	Pyflakes string
	// IgnorePatterns is list of regular expression to filter errors. The pattern is applied to error
	// messages. When an error is matched, the error is ignored.
	IgnorePatterns []string
	// ConfigFile is a path to config file. Empty string means no config file path is given. In
	// the case, actionlint will try to read config from .github/actionlint.yaml.
	ConfigFile string
	// ConfigOverlays apply in order over each project's selected config file.
	ConfigOverlays []ConfigOverlay
	// OnConfigLoaded reports actual config sources once per project, outside the cache lock.
	// Callbacks may reenter the session; concurrent callers must synchronize callback state.
	OnConfigLoaded func(ConfigReport)
	// Format is a custom template to format error messages. It must follow Go Template format and
	// contain at least one {{ }} placeholder. https://pkg.go.dev/text/template
	Format string
	// OutputFormat selects a built-in renderer and cannot be combined with Format.
	// JSON emits CheckResult; JSONL emits individual Diagnostic records.
	OutputFormat OutputFormat
	// StdinFileName is a file name when reading input from stdin. When this value is empty, "<stdin>"
	// is used as the default value.
	StdinFileName string
	// WorkingDir is a file path to the current working directory. When this value is empty, os.Getwd
	// will be used to get a working directory.
	WorkingDir string
	// OnRulesCreated adds or removes rules for each workflow input. It receives
	// the built-in rules and returns the rules to run. Standalone action metadata
	// uses built-in metadata and script checks and does not call this hook.
	// Note that syntax errors may be reported even if this function returns nil or an empty slice.
	OnRulesCreated func([]Rule) []Rule
	// OnFilesSelected is called with the exact file set passed to LintFiles. The callback receives
	// a copy so modifying it does not affect linting.
	OnFilesSelected func([]string)
	// Context bounds the lifetime of the linting. Cancelling it kills the external
	// child processes which are running. When this value is nil, context.Background() is used.
	Context context.Context
	// Ruff selects the executable for Python run scripts. Empty disables it.
	Ruff string
	// ShellcheckRunner supplies an isolated embedded ShellCheck backend.
	ShellcheckRunner func([]string, string) ([]byte, error)
	// More options will come here
}

// Linter is struct to lint workflow files.
type Linter struct {
	*AnalysisSession
	out      io.Writer
	renderer *AnalysisRenderer
}

// NewLinter creates a compatibility facade with caller-selected output and options.
// Set out to io.Discard to return findings without printing them.
func NewLinter(out io.Writer, opts *LinterOptions) (*Linter, error) {
	out = LegacyColorOutput(out, opts.Color)
	app, err := newLegacyAnalysisSession(opts)
	if err != nil {
		return nil, err
	}
	renderer, err := NewAnalysisRenderer(opts.OutputFormat, opts.Format, opts.Oneline)
	if err != nil {
		return nil, err
	}
	app.debug("Create a Linter instance with option %#v", opts)
	return &Linter{AnalysisSession: app, out: out, renderer: renderer}, nil
}

// LegacyColorOutput preserves NewLinter's process-wide color setting and Windows writer.
func LegacyColorOutput(out io.Writer, option ColorOptionKind) io.Writer {
	if option == ColorOptionKindNever {
		color.NoColor = true
	} else {
		if option == ColorOptionKindAlways {
			color.NoColor = false
		}
		if f, ok := out.(*os.File); ok {
			out = colorable.NewColorable(f)
		}
	}
	return out
}

// GenerateDefaultConfig generates default config file at ".github/actionlint.yaml" in the project
// which the given directory path belongs to. When the directory path is empty, the current directory
// will be used instead.
func (l *Linter) GenerateDefaultConfig(dir string) error {
	p, err := l.generateDefaultConfig(dir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(l.out, "Config file was generated at %q\n", p)
	return err
}

func (l *Linter) generateDefaultConfig(dir string) (string, error) {
	if dir == "" {
		dir = l.WorkingDir()
	}

	l.log("Generating default actionlint.yaml in repository:", dir)
	return GenerateProjectConfig(dir, false)
}

func generateProjectConfig(projects *Projects, dir string) (string, error) {
	project, err := projects.At(dir)
	if err != nil {
		return "", err
	}
	root := ""
	if project != nil {
		root = project.RootDir()
	}
	return repoconfig.Generate(root, true, Version())
}

// LintRepository finds workflows and actions in the nearest project and prints their findings.
// An empty directory starts discovery at LinterOptions.WorkingDir.
func (l *Linter) LintRepository(dir string) ([]*Error, error) {
	return l.report(l.Repository(dir))
}

// LintDir checks YAML workflows recursively in dir, in sorted path order.
func (l *Linter) LintDir(dir string, project *Project) ([]*Error, error) {
	return l.report(l.directory(dir, project))
}

// LintFiles checks paths in the supplied order. A nil project enables per-file discovery.
func (l *Linter) LintFiles(paths []string, project *Project) ([]*Error, error) {
	return l.report(l.Files(paths, project))
}

// LintFile reads and checks one workflow. A nil project enables discovery from path.
func (l *Linter) LintFile(path string, project *Project) ([]*Error, error) {
	return l.report(l.readFiles([]string{path}, project))
}

// LintStdin checks the reader using LinterOptions.StdinFileName, or <stdin> by default.
func (l *Linter) LintStdin(stdin io.Reader) ([]*Error, error) {
	return l.report(l.ReadStdin(stdin, false))
}

// Lint checks content without reading path. An existing path can identify its project.
func (l *Linter) Lint(path string, content []byte, project *Project) ([]*Error, error) {
	return l.report(l.content(path, content, project, false))
}

func (l *Linter) report(result *AnalysisResult, err error) ([]*Error, error) {
	if err != nil {
		return nil, LegacyAnalysisError(err)
	}
	// LintFiles with no paths has always returned an empty slice without output.
	if len(result.files) != 0 {
		if err := l.renderer.Render(l.out, result); err != nil {
			return nil, err
		}
	}
	l.Completed(result)
	return result.legacyErrors(), nil
}

// LegacyAnalysisError preserves the single-file and batch error messages of Lint methods.
func LegacyAnalysisError(err error) error {
	if source, ok := errors.AsType[*sourceAnalysisError](err); ok {
		if source.batch {
			return fmt.Errorf("fatal error while checking %s: %w", source.path, source.err)
		}
		return source.err
	}
	return err
}

func newLegacyAnalysisSession(opts *LinterOptions) (*AnalysisSession, error) {
	return NewAnalysisSession(AnalysisOptions{
		Context: opts.Context, WorkingDir: opts.WorkingDir, StdinFileName: opts.StdinFileName,
		ConfigFile: opts.ConfigFile, ConfigOverlays: opts.ConfigOverlays, OnConfigLoaded: opts.OnConfigLoaded, Shellcheck: opts.Shellcheck, ShellcheckRunner: opts.ShellcheckRunner,
		Ruff:           opts.Ruff,
		IgnorePatterns: opts.IgnorePatterns, Verbose: opts.Verbose, Debug: opts.Debug,
		LogWriter: opts.LogWriter, OnRulesCreated: opts.OnRulesCreated, OnFilesSelected: opts.OnFilesSelected,
	})
}

// GenerateProjectConfig creates a repository config and returns its path without printing.
// skipConfig permits creation checks without first parsing an existing config.
func GenerateProjectConfig(dir string, skipConfig bool) (string, error) {
	return generateProjectConfig(workspace.Discoverer(skipConfig), dir)
}
