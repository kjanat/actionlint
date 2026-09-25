package actionlint

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"
)

// SourceUnit is a workflow and the configuration resolved for it by the caller.
type SourceUnit struct {
	Path    string
	Content []byte
	Config  *Config
	Project *Project
	// inputPath retains the opened path when Path is made relative for diagnostics.
	inputPath string
}

// AnalysisRequest contains resolved sources and analysis settings.
type AnalysisRequest struct {
	Sources            []SourceUnit
	ShellCheck         string
	Pyflakes           string
	ShellcheckOptions  *ExternalCommandOptions
	ShellcheckSettings *ShellcheckSettings
	PyflakesOptions    *ExternalCommandOptions
	IgnorePatterns     IgnorePatterns
	OnRulesCreated     func([]Rule) []Rule
	// WorkingDir resolves workflow paths in reusable-workflow caches. Empty uses os.Getwd.
	WorkingDir string
	// ReadFile reads local action metadata and reusable workflows. Nil uses os.ReadFile.
	// It must support concurrent calls and does not restrict external tools.
	ReadFile func(string) ([]byte, error)
}

// AnalysisResult contains findings, their sources, and every local input read during analysis.
type AnalysisResult struct {
	Diagnostics []Diagnostic
	Inputs      []string
	files       []analyzedFile
}

type analyzedFile struct {
	source SourceUnit
	errors []*Error
	rules  []Rule
}

type inputFiles struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (f *inputFiles) add(path string) {
	if path == "" || path == "<stdin>" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paths == nil {
		f.paths = map[string]struct{}{}
	}
	f.paths[absPath(path)] = struct{}{}
}

func (f *inputFiles) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(f.paths))
	for path := range f.paths {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// Analyze checks resolved workflows and returns data without rendering diagnostics.
func Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResult, error) {
	return analyze(ctx, request, io.Discard, LogLevelNone)
}

func analyze(ctx context.Context, request AnalysisRequest, log io.Writer, level LogLevel) (*AnalysisResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if level != LogLevelNone {
		log = &analysisLogWriter{out: log}
	}
	engine := &analysisEngine{ctx: ctx, shellcheck: request.ShellCheck, pyflakes: request.Pyflakes,
		shellcheckOptions: request.ShellcheckOptions, pyflakesOptions: request.PyflakesOptions,
		shellcheckSettings: request.ShellcheckSettings,
		ignorePats:         request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, analysisLogger: analysisLogger{log, level}}
	inputs := &inputFiles{}
	proc := newConcurrentProcess(ctx, runtime.NumCPU())
	actions := NewLocalActionsCacheFactory(engine.debugWriter())
	cwd := request.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	workflows := NewLocalReusableWorkflowCacheFactory(cwd, engine.debugWriter())
	engine.workingDir, engine.inputs = absPath(cwd), inputs
	engine.gitModes = &gitModes{}
	result := &AnalysisResult{Diagnostics: []Diagnostic{}, files: make([]analyzedFile, len(request.Sources))}
	readFile := request.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	// Initialize shared caches before any analysis goroutines access them.
	for _, source := range request.Sources {
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(source.Project)
		ac.onRead, wc.onRead = inputs.add, inputs.add
		ac.readFile, wc.readFile = readFile, readFile
	}
	group := errgroup.Group{}
	group.SetLimit(runtime.NumCPU())
	for i, source := range request.Sources {
		path := source.inputPath
		if path == "" {
			path = source.Path
		}
		inputs.add(path)
		if source.Project != nil {
			inputs.add(source.Project.configPath)
		}
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(source.Project)
		group.Go(func() error {
			file := &result.files[i]
			file.source = source
			var err error
			file.errors, err = engine.check(source.Path, source.Content, source.Project, source.Config, proc, ac, wc, &file.rules)
			if err != nil {
				return &sourceAnalysisError{path: source.Path, err: err, batch: len(request.Sources) > 1}
			}
			return err
		})
	}
	err := group.Wait()
	proc.wait()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.collectDiagnostics()
	result.Inputs = inputs.list()
	return result, nil
}

func (r *AnalysisResult) collectDiagnostics() {
	type location struct {
		project, path, rule string
		start               DiagnosticPosition
	}
	seen := make(map[location][]Diagnostic)
	for i := range r.files {
		file := &r.files[i]
		project := ""
		if file.source.Project != nil {
			project = absPath(file.source.Project.RootDir())
		}
		unique := file.errors[:0]
		for _, finding := range file.errors {
			diagnostic := finding.diagnostic(file.source.Content)
			key := location{project, diagnostic.Path, diagnostic.Rule, diagnostic.Start}
			if slices.ContainsFunc(seen[key], func(previous Diagnostic) bool {
				return reflect.DeepEqual(previous, diagnostic)
			}) {
				continue
			}
			seen[key] = append(seen[key], diagnostic)
			r.Diagnostics = append(r.Diagnostics, diagnostic)
			unique = append(unique, finding)
		}
		clear(file.errors[len(unique):])
		file.errors = unique
	}
}

type sourceAnalysisError struct {
	batch bool
	path  string
	err   error
}

func (e *sourceAnalysisError) Error() string { return e.err.Error() }
func (e *sourceAnalysisError) Unwrap() error { return e.err }

func (r *AnalysisResult) legacyErrors() []*Error {
	if len(r.files) == 1 {
		return r.files[0].errors
	}
	errors := make([]*Error, 0, len(r.Diagnostics))
	for _, file := range r.files {
		errors = append(errors, file.errors...)
	}
	return errors
}

// CompileIgnorePatterns validates message filters used by analysis and config initialization.
func CompileIgnorePatterns(patterns []string) (IgnorePatterns, error) {
	ignore := make(IgnorePatterns, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression for ignore pattern %q: %w", pattern, err)
		}
		ignore = append(ignore, re)
	}
	return ignore, nil
}

// FileCount reports the number of workflows analyzed, including those with no findings.
func (r *AnalysisResult) FileCount() int { return len(r.files) }
