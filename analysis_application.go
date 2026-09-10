package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// analysisApplication resolves local inputs before handing them to Analyze.
// It retains project discovery state for callers that reuse a Linter.
type analysisApplication struct {
	analysisLogger
	projects        *Projects
	defaultConfig   *Config
	request         AnalysisRequest
	ctx             context.Context
	cwd             string
	stdin           string
	onFilesSelected func([]string)
	logSelection    bool
}

type analysisLogger struct {
	logOut   io.Writer
	logLevel LogLevel
}

func (l *analysisLogger) log(args ...any) {
	if l.logLevel >= LogLevelVerbose {
		_, _ = fmt.Fprint(l.logOut, "verbose: ")
		_, _ = fmt.Fprintln(l.logOut, args...)
	}
}

func (l *analysisLogger) debug(format string, args ...any) {
	if l.logLevel >= LogLevelDebug {
		_, _ = fmt.Fprintf(l.logOut, "[Linter] "+format+"\n", args...)
	}
}

func (l *analysisLogger) debugWriter() io.Writer {
	if l.logLevel < LogLevelDebug {
		return nil
	}
	return l.logOut
}

func newAnalysisApplication(opts *LinterOptions) (*analysisApplication, error) {
	a := &analysisApplication{
		projects: NewProjects(), ctx: opts.Context, cwd: opts.WorkingDir,
		stdin: opts.StdinFileName, onFilesSelected: opts.OnFilesSelected,
		logSelection:   true,
		request:        AnalysisRequest{ShellCheck: opts.Shellcheck, Pyflakes: opts.Pyflakes, OnRulesCreated: opts.OnRulesCreated},
		analysisLogger: analysisLogger{logOut: opts.LogWriter},
	}
	if a.ctx == nil {
		a.ctx = context.Background()
	}
	if a.logOut == nil {
		a.logOut = io.Discard
	}
	if opts.Verbose {
		a.logLevel = LogLevelVerbose
	} else if opts.Debug {
		a.logLevel = LogLevelDebug
	}
	var err error
	if opts.ConfigFile != "" {
		a.defaultConfig, err = ReadConfigFile(opts.ConfigFile)
		if err != nil {
			return nil, err
		}
	}
	a.request.IgnorePatterns, err = compileIgnorePatterns(opts.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	if a.cwd == "" {
		a.cwd, err = os.Getwd()
		if err != nil {
			a.cwd = "."
		}
	}
	if a.stdin == "" {
		a.stdin = "<stdin>"
	}
	a.request.WorkingDir = a.cwd
	return a, nil
}

func (a *analysisApplication) selectionLog(args ...any) {
	if a.logSelection {
		a.log(args...)
	}
}

func (a *analysisApplication) repository(dir string) (*AnalysisResult, error) {
	if dir == "" {
		dir = a.cwd
	}
	a.selectionLog("Linting all workflow files in repository:", dir)
	project, err := a.projects.At(dir)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("no project was found in any parent directories of %q. check workflows directory is put correctly in your Git repository", dir)
	}
	a.selectionLog("Detected project:", project.RootDir())
	return a.directory(project.WorkflowsDir(), project)
}

func (a *analysisApplication) directory(dir string, project *Project) (*AnalysisResult, error) {
	paths := []string{}
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not read files in %q: %w", dir, err)
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		a.filesSelected(paths)
		return nil, fmt.Errorf("no YAML file was found in %q", dir)
	}
	a.selectionLog("Collected", len(paths), "YAML files")
	return a.files(paths, project)
}

func (a *analysisApplication) filesSelected(paths []string) {
	if a.onFilesSelected != nil {
		a.onFilesSelected(slices.Clone(paths))
	}
}

func (a *analysisApplication) files(paths []string, project *Project) (*AnalysisResult, error) {
	a.filesSelected(paths)
	if len(paths) == 0 {
		return &AnalysisResult{Diagnostics: []Diagnostic{}}, nil
	}
	if len(paths) > 1 {
		a.selectionLog("Linting", len(paths), "files")
	}
	return a.readFiles(paths, project)
}

func (a *analysisApplication) readFiles(paths []string, project *Project) (*AnalysisResult, error) {
	sources := make([]SourceUnit, 0, len(paths))
	for _, path := range paths {
		proj := project
		if proj == nil {
			var err error
			proj, err = a.projects.At(path)
			if err != nil {
				return nil, err
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q: %w", path, err)
		}
		source := a.source(a.relativePath(path), content, proj)
		source.inputPath = path
		sources = append(sources, source)
	}
	return a.analyze(sources)
}

func (a *analysisApplication) relativePath(path string) string {
	if rel, err := filepath.Rel(a.cwd, path); err == nil {
		return rel
	}
	return path
}

func (a *analysisApplication) readStdin(stdin io.Reader, normalizePath bool) (*AnalysisResult, error) {
	a.selectionLog("Reading the input from stdin")
	content, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("could not read stdin: %w", err)
	}
	return a.content(a.stdin, content, nil, normalizePath)
}

func (a *analysisApplication) content(path string, content []byte, project *Project, normalizePath bool) (*AnalysisResult, error) {
	if project == nil && path != "<stdin>" {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			var err error
			project, err = a.projects.At(path)
			if err != nil {
				return nil, err
			}
		}
	}
	source := a.source(path, content, project)
	source.inputPath = path
	if normalizePath {
		source.Path = a.relativePath(path)
	}
	return a.analyze([]SourceUnit{source})
}

func (a *analysisApplication) source(path string, content []byte, project *Project) SourceUnit {
	cfg := a.defaultConfig
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	return SourceUnit{Path: path, Content: content, Config: cfg, Project: project}
}

func (a *analysisApplication) analyze(sources []SourceUnit) (*AnalysisResult, error) {
	request := a.request
	request.Sources = sources
	result, err := analyze(a.ctx, request, a.logOut, a.logLevel)
	if err != nil {
		return nil, err
	}
	for _, project := range a.projects.known {
		if project.Config() != nil {
			for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
				result.Inputs = append(result.Inputs, filepath.Join(project.RootDir(), ".github", name))
			}
		}
	}
	return result, nil
}

func (a *analysisApplication) completed(result *AnalysisResult) {
	if len(result.files) > 1 {
		a.selectionLog("Found", len(result.Diagnostics), "errors in", len(result.files), "files")
	}
}
