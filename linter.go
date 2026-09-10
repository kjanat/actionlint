package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
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
	projects        *Projects
	out             io.Writer
	logOut          io.Writer
	logLevel        LogLevel
	oneline         bool
	shellcheck      string
	pyflakes        string
	ignorePats      IgnorePatterns
	stdin           string
	defaultConfig   *Config
	errFmt          diagnosticFormatter
	cwd             string
	onRulesCreated  func([]Rule) []Rule
	onFilesSelected func([]string)
	ctx             context.Context
	inputs          *inputFiles
}

// NewLinter creates a new Linter instance.
// The out parameter is used to output errors from Linter instance. Set io.Discard if you don't
// want the outputs.
// The opts parameter is LinterOptions instance which configures behavior of linting.
func NewLinter(out io.Writer, opts *LinterOptions) (*Linter, error) {
	level := LogLevelNone
	if opts.Verbose {
		level = LogLevelVerbose
	} else if opts.Debug {
		level = LogLevelDebug
	}

	if opts.Color == ColorOptionKindNever {
		color.NoColor = true
	} else {
		if opts.Color == ColorOptionKindAlways {
			color.NoColor = false
		}
		// Allow colorful output on Windows
		if f, ok := out.(*os.File); ok {
			out = colorable.NewColorable(f)
		}
	}

	lout := io.Discard
	if opts.LogWriter != nil {
		lout = opts.LogWriter
	}

	var cfg *Config
	if opts.ConfigFile != "" {
		c, err := ReadConfigFile(opts.ConfigFile)
		if err != nil {
			return nil, err
		}
		cfg = c
	}

	ignore, err := compileIgnorePatterns(opts.IgnorePatterns)
	if err != nil {
		return nil, err
	}

	format := opts.Format
	oneline := opts.Oneline
	if opts.OutputFormat != "" && format != "" {
		return nil, errors.New("OutputFormat cannot be combined with a custom Format template")
	}
	var formatter diagnosticFormatter
	switch opts.OutputFormat {
	case "", OutputFormatText:
	case OutputFormatOneline:
		oneline = true
	case OutputFormatJSON, OutputFormatJSONL:
		formatter = jsonDiagnosticFormatter{lines: opts.OutputFormat == OutputFormatJSONL}
	case OutputFormatSARIF:
		format = SARIFTemplate()
	case OutputFormatGitHub:
		formatter = githubDiagnosticFormatter{}
	default:
		return nil, fmt.Errorf("unknown output format %q", opts.OutputFormat)
	}
	if format != "" {
		f, err := NewErrorFormatter(format)
		if err != nil {
			return nil, err
		}
		formatter = f
	}

	cwd := "."
	if opts.WorkingDir != "" {
		cwd = opts.WorkingDir
	} else if d, err := os.Getwd(); err == nil {
		cwd = d
	}

	stdin := "<stdin>"
	if opts.StdinFileName != "" {
		stdin = opts.StdinFileName
	}

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	l := &Linter{
		NewProjects(),
		out,
		lout,
		level,
		oneline,
		opts.Shellcheck,
		opts.Pyflakes,
		ignore,
		stdin,
		cfg,
		formatter,
		cwd,
		opts.OnRulesCreated,
		opts.OnFilesSelected,
		ctx,
		&inputFiles{},
	}

	l.debug("Create a Linter instance with option %#v", opts)
	return l, nil
}

func (l *Linter) log(args ...any) {
	if l.logLevel < LogLevelVerbose {
		return
	}
	_, _ = fmt.Fprint(l.logOut, "verbose: ")
	_, _ = fmt.Fprintln(l.logOut, args...)
}

func (l *Linter) debug(format string, args ...any) {
	if l.logLevel < LogLevelDebug {
		return
	}
	format = "[Linter] " + format + "\n"
	_, _ = fmt.Fprintf(l.logOut, format, args...)
}

func (l *Linter) debugWriter() io.Writer {
	if l.logLevel < LogLevelDebug {
		return nil
	}
	return l.logOut
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

// LintRepository lints YAML workflow files and outputs the errors to given writer. It finds the
// nearest `.github/workflows` directory based on `dir` and applies lint rules to all YAML workflow
// files under the directory. When the directory path is empty, the current working directory will
// be used instead.
func (l *Linter) LintRepository(dir string) ([]*Error, error) {
	if dir == "" {
		dir = l.cwd
	}

	l.log("Linting all workflow files in repository:", dir)

	p, err := l.projects.At(dir)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("no project was found in any parent directories of %q. check workflows directory is put correctly in your Git repository", dir)
	}

	l.log("Detected project:", p.RootDir())
	wd := p.WorkflowsDir()
	return l.LintDir(wd, p)
}

// LintDir lints all YAML workflow files in the given directory recursively.
func (l *Linter) LintDir(dir string, project *Project) ([]*Error, error) {
	files := []string{}
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not read files in %q: %w", dir, err)
	}

	// To make output deterministic, sort order of file paths
	sort.Strings(files)
	if len(files) == 0 {
		l.filesSelected(files)
		return nil, fmt.Errorf("no YAML file was found in %q", dir)
	}
	l.log("Collected", len(files), "YAML files")

	return l.LintFiles(files, project)
}

func (l *Linter) filesSelected(filepaths []string) {
	if l.onFilesSelected != nil {
		l.onFilesSelected(slices.Clone(filepaths))
	}
}

// LintFiles lints YAML workflow files and outputs the errors to given writer. It applies lint
// rules to all given files. The project parameter can be nil. In the case, a project is detected
// from the file path.
func (l *Linter) LintFiles(filepaths []string, project *Project) ([]*Error, error) {
	l.filesSelected(filepaths)
	n := len(filepaths)
	switch n {
	case 0:
		return []*Error{}, nil
	case 1:
		return l.LintFile(filepaths[0], project)
	}

	l.log("Linting", n, "files")

	cwd := l.cwd
	cpus := runtime.NumCPU()
	proc := newConcurrentProcess(l.ctx, cpus)
	sema := semaphore.NewWeighted(int64(cpus))
	dbg := l.debugWriter()
	acf := NewLocalActionsCacheFactory(dbg)
	rwcf := NewLocalReusableWorkflowCacheFactory(cwd, dbg)

	type workspace struct {
		path string
		errs []*Error
		src  []byte
	}

	ws := make([]workspace, 0, len(filepaths))
	for _, p := range filepaths {
		ws = append(ws, workspace{path: p})
	}

	eg := errgroup.Group{}
	for i := range ws {
		// Each element of ws is accessed by single goroutine so mutex is unnecessary
		w := &ws[i]
		proj := project
		if proj == nil {
			// This method modifies state of l.projects so it cannot be called in parallel.
			// Before entering goroutine, resolve project instance.
			p, err := l.projects.At(w.path)
			if err != nil {
				return nil, err
			}
			proj = p
		}
		ac := acf.GetCache(proj) // #173
		rwc := rwcf.GetCache(proj)
		if ac.onRead == nil {
			ac.onRead = l.inputs.add
		}
		if rwc.onRead == nil {
			rwc.onRead = l.inputs.add
		}

		eg.Go(func() error {
			// Bound concurrency on reading files to avoid "too many files to open" error (issue #3)
			if err := sema.Acquire(l.ctx, 1); err != nil {
				return err
			}
			src, err := os.ReadFile(w.path)
			sema.Release(1)
			if err != nil {
				return fmt.Errorf("could not read %q: %w", w.path, err)
			}

			if cwd != "" {
				if r, err := filepath.Rel(cwd, w.path); err == nil {
					w.path = r // Use relative path if possible
				}
			}
			errs, err := l.check(w.path, src, proj, proc, ac, rwc)
			if err != nil {
				return fmt.Errorf("fatal error while checking %s: %w", w.path, err)
			}
			w.src = src
			w.errs = errs
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Ensure that all processes finish. `proc.wait()` must be called after `eg.Wait()`.
	// Calling `WaitGroup.Add` after `WaitGroup.Wait` can cause a race condition (specifically when
	// increasing the group count from 0 to 1 and calling `Wait` and at the same time).
	// `WaitGroup.Add` is called in `proc.run()` and `WaitGroup.Wait` is called in `proc.wait()`.
	// After traversing all workflows, `proc.run()` is no longer called so `proc.wait()` can be
	// called safely.
	proc.wait()

	total := 0
	for i := range ws {
		total += len(ws[i].errs)
	}

	all := make([]*Error, 0, total)
	if native, ok := l.errFmt.(jsonDiagnosticFormatter); ok {
		diagnostics := []Diagnostic{}
		for _, w := range ws {
			for _, e := range w.errs {
				diagnostics = append(diagnostics, e.diagnostic(w.src))
			}
			all = append(all, w.errs...)
		}
		if err := writeDiagnostics(l.out, diagnostics, native.lines); err != nil {
			return nil, err
		}
	} else if l.errFmt != nil {
		temp := make([]*ErrorTemplateFields, 0, total)
		for i := range ws {
			w := &ws[i]
			for _, err := range w.errs {
				temp = append(temp, err.GetTemplateFields(w.src))
			}
			all = append(all, w.errs...)
		}
		if err := l.errFmt.Print(l.out, temp); err != nil {
			return nil, err
		}
	} else {
		for i := range ws {
			w := &ws[i]
			l.printErrors(w.errs, w.src)
			all = append(all, w.errs...)
		}
	}

	l.log("Found", total, "errors in", n, "files")

	return all, nil
}

// LintFile lints one YAML workflow file and outputs the errors to given writer. The project
// parameter can be nil. In the case, the project is detected from the given path.
func (l *Linter) LintFile(path string, project *Project) ([]*Error, error) {
	if project == nil {
		p, err := l.projects.At(path)
		if err != nil {
			return nil, err
		}
		project = p
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %w", path, err)
	}

	if l.cwd != "" {
		if r, err := filepath.Rel(l.cwd, path); err == nil {
			path = r
		}
	}

	proc := newConcurrentProcess(l.ctx, runtime.NumCPU())
	dbg := l.debugWriter()
	localActions := NewLocalActionsCache(project, dbg)
	localReusableWorkflows := NewLocalReusableWorkflowCache(project, l.cwd, dbg)
	localActions.onRead, localReusableWorkflows.onRead = l.inputs.add, l.inputs.add
	errs, err := l.check(path, src, project, proc, localActions, localReusableWorkflows)
	proc.wait()
	if err != nil {
		return nil, err
	}

	if l.errFmt != nil {
		if err := l.errFmt.PrintErrors(l.out, errs, src); err != nil {
			return nil, err
		}
	} else {
		l.printErrors(errs, src)
	}
	return errs, err
}

// LintStdin lints the content read from STDIN. The stdin parameter is a reader to read from STDIN,
// which is usually os.Stdin. The file name is determined by LinterOptions.StdinFileName. When the
// option is empty, "<stdin>" is the default value.
func (l *Linter) LintStdin(stdin io.Reader) ([]*Error, error) {
	l.log("Reading the input from stdin")
	b, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("could not read stdin: %w", err)
	}
	return l.Lint(l.stdin, b, nil)
}

// Lint lints YAML workflow file content given as byte slice. The path parameter is used as file
// path where the content came from.
// When nil is passed to the project parameter, it tries to find the project from the path parameter.
func (l *Linter) Lint(path string, content []byte, project *Project) ([]*Error, error) {
	if project == nil && path != "<stdin>" {
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			p, err := l.projects.At(path)
			if err != nil {
				return nil, err
			}
			project = p
		}
	}
	proc := newConcurrentProcess(l.ctx, runtime.NumCPU())
	dbg := l.debugWriter()
	localActions := NewLocalActionsCache(project, dbg)
	localReusableWorkflows := NewLocalReusableWorkflowCache(project, l.cwd, dbg)
	localActions.onRead, localReusableWorkflows.onRead = l.inputs.add, l.inputs.add
	errs, err := l.check(path, content, project, proc, localActions, localReusableWorkflows)
	proc.wait()
	if err != nil {
		return nil, err
	}
	if l.errFmt != nil {
		if err := l.errFmt.PrintErrors(l.out, errs, content); err != nil {
			return nil, err
		}
	} else {
		l.printErrors(errs, content)
	}
	return errs, nil
}

func (l *Linter) check(path string, content []byte, project *Project, proc *concurrentProcess, actions *LocalActionsCache, workflows *LocalReusableWorkflowCache) ([]*Error, error) {
	l.inputs.add(path)
	cfg := l.defaultConfig
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	engine := &analysisEngine{ctx: l.ctx, shellcheck: l.shellcheck, pyflakes: l.pyflakes,
		ignorePats: l.ignorePats, onRulesCreated: l.onRulesCreated, logOut: l.logOut, logLevel: l.logLevel}
	var rules []Rule
	errs, err := engine.check(path, content, project, cfg, proc, actions, workflows, &rules)
	if formatter, ok := l.errFmt.(*ErrorFormatter); ok {
		for _, rule := range rules {
			formatter.RegisterRule(rule)
		}
	}
	return errs, err
}

func (l *Linter) printErrors(errs []*Error, src []byte) {
	if l.oneline {
		src = nil
	}
	for _, err := range errs {
		err.PrettyPrint(l.out, src)
	}
}
