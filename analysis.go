package actionlint

import (
	"context"
	"fmt"
	"io"
	"os"
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
}

// AnalysisRequest contains resolved sources and analysis settings, not CLI flags or renderers.
type AnalysisRequest struct {
	Sources        []SourceUnit
	ShellCheck     string
	Pyflakes       string
	IgnorePatterns IgnorePatterns
	OnRulesCreated func([]Rule) []Rule
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
	engine := &analysisEngine{ctx: ctx, shellcheck: request.ShellCheck, pyflakes: request.Pyflakes,
		ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, logOut: log, logLevel: level}
	inputs := &inputFiles{}
	proc := newConcurrentProcess(ctx, runtime.NumCPU())
	actions := NewLocalActionsCacheFactory(engine.debugWriter())
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	workflows := NewLocalReusableWorkflowCacheFactory(cwd, engine.debugWriter())
	result := &AnalysisResult{Diagnostics: []Diagnostic{}, files: make([]analyzedFile, len(request.Sources))}
	// Initialize shared caches before any analysis goroutines access them.
	for _, source := range request.Sources {
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(source.Project)
		ac.onRead, wc.onRead = inputs.add, inputs.add
	}
	group := errgroup.Group{}
	group.SetLimit(runtime.NumCPU())
	for i, source := range request.Sources {
		inputs.add(source.Path)
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(source.Project)
		group.Go(func() error {
			file := &result.files[i]
			file.source = source
			var err error
			file.errors, err = engine.check(source.Path, source.Content, source.Project, source.Config, proc, ac, wc, &file.rules)
			return err
		})
	}
	err = group.Wait()
	proc.wait()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, file := range result.files {
		for _, finding := range file.errors {
			result.Diagnostics = append(result.Diagnostics, finding.diagnostic(file.source.Content))
		}
	}
	result.Inputs = inputs.list()
	return result, nil
}

func compileIgnorePatterns(patterns []string) (IgnorePatterns, error) {
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
