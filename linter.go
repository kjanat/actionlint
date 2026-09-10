package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

// LogLevel is log level of logger used in Linter instance.
type LogLevel int

const (
	// LogLevelNone does not output any log output.
	LogLevelNone LogLevel = 0
	// LogLevelVerbose shows verbose log output. This is equivalent to specifying -verbose option
	// to actionlint command.
	LogLevelVerbose = 1
	// LogLevelDebug shows all log output including debug information.
	LogLevelDebug = 2
)

// ColorOptionKind is kind of colorful output behavior.
type ColorOptionKind int

const (
	// ColorOptionKindAuto is kind to determine to colorize errors output automatically. It is
	// determined based on pty and $NO_COLOR environment variable. See document of fatih/color
	// for more details.
	ColorOptionKindAuto ColorOptionKind = iota
	// ColorOptionKindAlways is kind to always colorize errors output.
	ColorOptionKindAlways
	// ColorOptionKindNever is kind never to colorize errors output.
	ColorOptionKindNever
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
	// Pyflakes is executable for running pyflakes external command. It can be command name like "pyflakes"
	// or file path like "/path/to/pyflakes", "path/to/pyflakes". When this value is empty, pyflakes
	// won't run to check scripts in workflow file.
	Pyflakes string
	// IgnorePatterns is list of regular expression to filter errors. The pattern is applied to error
	// messages. When an error is matched, the error is ignored.
	IgnorePatterns []string
	// ConfigFile is a path to config file. Empty string means no config file path is given. In
	// the case, actionlint will try to read config from .github/actionlint.yaml.
	ConfigFile string
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
	// OnRulesCreated is a hook to add or remove the check rules. This function is called on checking
	// every workflow files. Rules created by Linter instance are passed to the argument and the
	// function should return the modified rules.
	// Note that syntax errors may be reported even if this function returns nil or an empty slice.
	OnRulesCreated func([]Rule) []Rule
	// OnFilesSelected is called with the exact file set passed to LintFiles. The callback receives
	// a copy so modifying it does not affect linting.
	OnFilesSelected func([]string)
	// Context bounds the lifetime of the linting. Cancelling it kills the shellcheck and pyflakes
	// child processes which are running. When this value is nil, context.Background() is used.
	Context context.Context
	// More options will come here
}

// Linter is struct to lint workflow files.
type Linter struct {
	*analysisApplication
	out      io.Writer
	renderer *analysisRenderer
}

// NewLinter creates a compatibility facade with caller-selected output and options.
// Set out to io.Discard to return findings without printing them.
func NewLinter(out io.Writer, opts *LinterOptions) (*Linter, error) {
	out = legacyColorOutput(out, opts.Color)
	app, err := newAnalysisApplication(opts)
	if err != nil {
		return nil, err
	}
	renderer, err := newAnalysisRenderer(opts.OutputFormat, opts.Format, opts.Oneline)
	if err != nil {
		return nil, err
	}
	app.debug("Create a Linter instance with option %#v", opts)
	return &Linter{analysisApplication: app, out: out, renderer: renderer}, nil
}

// legacyColorOutput preserves NewLinter's process-wide color setting and Windows writer.
func legacyColorOutput(out io.Writer, option ColorOptionKind) io.Writer {
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
		dir = l.cwd
	}

	l.log("Generating default actionlint.yaml in repository:", dir)
	return generateProjectConfig(l.projects, dir)
}

func generateProjectConfig(projects *Projects, dir string) (string, error) {
	proj, err := projects.At(dir)
	if err != nil {
		return "", err
	}
	if proj == nil {
		return "", errors.New("project is not found. check current project is initialized as Git repository and \".github/workflows\" directory exists")
	}

	d := filepath.Join(proj.RootDir(), ".github")
	for _, f := range []string{"actionlint.yaml", "actionlint.yml"} {
		p := filepath.Join(d, f)
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("config file already exists at %q", p)
		}
	}

	p := filepath.Join(d, "actionlint.yaml")
	if err := writeDefaultConfigFile(p); err != nil {
		return "", err
	}
	return p, nil
}

// LintRepository finds workflows in the nearest project and prints their findings.
// An empty directory starts discovery at LinterOptions.WorkingDir.
func (l *Linter) LintRepository(dir string) ([]*Error, error) {
	return l.report(l.repository(dir))
}

// LintDir checks YAML workflows recursively in dir, in sorted path order.
func (l *Linter) LintDir(dir string, project *Project) ([]*Error, error) {
	return l.report(l.directory(dir, project))
}

// LintFiles checks paths in the supplied order. A nil project enables per-file discovery.
func (l *Linter) LintFiles(paths []string, project *Project) ([]*Error, error) {
	return l.report(l.files(paths, project))
}

// LintFile reads and checks one workflow. A nil project enables discovery from path.
func (l *Linter) LintFile(path string, project *Project) ([]*Error, error) {
	return l.report(l.readFiles([]string{path}, project))
}

// LintStdin checks the reader using LinterOptions.StdinFileName, or <stdin> by default.
func (l *Linter) LintStdin(stdin io.Reader) ([]*Error, error) {
	return l.report(l.readStdin(stdin, false))
}

// Lint checks content without reading path. An existing path can identify its project.
func (l *Linter) Lint(path string, content []byte, project *Project) ([]*Error, error) {
	return l.report(l.content(path, content, project, false))
}

func (l *Linter) report(result *AnalysisResult, err error) ([]*Error, error) {
	if err != nil {
		return nil, legacyAnalysisError(err)
	}
	// LintFiles with no paths has always returned an empty slice without output.
	if len(result.files) != 0 {
		if err := l.renderer.render(l.out, result); err != nil {
			return nil, err
		}
	}
	l.completed(result)
	return result.legacyErrors(), nil
}

func legacyAnalysisError(err error) error {
	if source, ok := errors.AsType[*sourceAnalysisError](err); ok {
		if source.batch {
			return fmt.Errorf("fatal error while checking %s: %w", source.path, source.err)
		}
		return source.err
	}
	return err
}
