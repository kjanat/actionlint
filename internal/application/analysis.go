package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	documentmodel "actionlint.kjanat.dev/document"

	"actionlint.kjanat.dev/analyzer"
	diagmodel "actionlint.kjanat.dev/diagnostic"
	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/builtin"
	"actionlint.kjanat.dev/internal/checks"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/internal/documentparse"
	exprlang "actionlint.kjanat.dev/internal/expression"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/report"
	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/workflow/calls"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/rule"
	"actionlint.kjanat.dev/runner"

	"golang.org/x/sync/errgroup"
)

// sourceUnit is a workflow or action manifest and its resolved configuration.
type sourceUnit = workspace.Source

// analysisRequest contains resolved sources and analysis settings.
type analysisRequest struct {
	Runners          runner.Options
	Sources          []sourceUnit
	Analyzers        []analyzer.Registration
	Tools            []tools.Registration
	IgnorePatterns   repoconfig.IgnorePatterns
	OnRulesCreated   func([]workflowvisit.Rule) []workflowvisit.Rule
	WorkingDir       string
	ReadFile         func(string) ([]byte, error)
	RulePresets      repoconfig.Presets
	LoadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
	LogWriter        io.Writer
	LogLevel         LogLevel
	Processes        *analyzerhost.ProcessPool
}

// analysisResult contains findings, their sources, and every local input read during analysis.
type analysisResult struct {
	report.Analysis
	files []analyzedFile
}

type analyzedFile struct {
	source  sourceUnit
	errors  []*findingmodel.Finding
	rules   []rule.Descriptor
	outline documentmodel.Outline
}

type inputFiles struct{ workspace.Inputs }

func (f *inputFiles) add(path string) { f.Add(path) }
func (f *inputFiles) list() []string  { return f.List() }
func (f *inputFiles) addConfig(cfg *repoconfig.Config, project *workspace.Project) {
	f.AddConfig(cfg, project)
}

// runAnalysis checks resolved workflows and action manifests without rendering diagnostics.
// On analysis failure, a non-nil result retains findings collected before the failure.
func runAnalysis(ctx context.Context, request analysisRequest) (*analysisResult, error) {
	log := request.LogWriter
	if log == nil {
		log = io.Discard
	}
	return run(ctx, request, log, request.LogLevel)
}

func run(ctx context.Context, request analysisRequest, log io.Writer, level LogLevel) (*analysisResult, error) {
	if request.Processes == nil {
		return nil, errors.New("analysis requires an application-owned process pool")
	}
	var extensions []rule.Descriptor
	for _, registration := range request.Analyzers {
		if registration.Analyzer == nil {
			return nil, errors.New("nil analyzer registration")
		}
		extensions = append(extensions, registration.Analyzer.Descriptor().Descriptor)
	}
	seenTools := map[string]bool{}
	for _, registration := range request.Tools {
		name := registration.Descriptor.Name
		if seenTools[name] {
			return nil, fmt.Errorf("duplicate script tool %q", name)
		}
		seenTools[name] = true
		if builtin.Owns(name) {
			return nil, fmt.Errorf("script tool %q conflicts with a built-in analyzer", name)
		}
		if _, builtIn := rulecatalog.Find(name); !builtIn {
			extensions = append(extensions, registration.Descriptor)
		}
	}
	if _, err := rulecatalog.Combine(extensions...); err != nil {
		return nil, err
	}

	for _, registration := range request.Tools {
		if registration.Build == nil {
			return nil, fmt.Errorf("tool %q has no adapter", registration.Descriptor.Name)
		}
		if registration.Validate != nil {
			if err := registration.Validate(); err != nil {
				return nil, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.WorkingDir == "" {
		var err error
		request.WorkingDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	var err error
	request.WorkingDir, err = filepath.Abs(request.WorkingDir)
	if err != nil {
		return nil, err
	}
	inputs := &inputFiles{}
	allSources := request.Sources
	selected := make([]sourceUnit, 0, len(request.Sources))
	for _, source := range request.Sources {
		// Selection itself consumes configuration, even when no source survives.
		inputs.addConfig(source.Config, source.Project)
		root := request.WorkingDir
		if source.Project != nil {
			root = source.Project.RootDir()
		}
		path := source.InputPath
		if path == "" {
			path = source.Path
		}
		if path != "<stdin>" && !filepath.IsAbs(path) {
			path = filepath.Join(request.WorkingDir, path)
		}
		source.InputPath = path
		if source.Config.IncludesFile(path, root) {
			selected = append(selected, source)
		}
	}
	request.Sources = selected
	if level != LogLevelNone {
		log = &analysisLogWriter{out: log}
	}
	logger := analysisLogger{log, level}
	session := &analysisSession{
		runners:   request.Runners,
		analyzers: request.Analyzers, ctx: ctx, toolDescriptors: extensions,
		tools: request.Tools, loadScriptEngine: request.LoadScriptEngine, rulePresets: request.RulePresets,
		ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, analysisLogger: logger,
	}

	documents := &actionDocuments{byPath: map[string]documentmodel.Action{}}
	proc := request.Processes
	actions := actionlocal.NewFactory(session.debugWriter())
	cwd := request.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	workflows := calls.NewCacheFactory(cwd, session.debugWriter())
	reader := fsread.New(ctx, request.ReadFile, inputs.add)
	session.workingDir, session.reader = absPath(cwd), reader
	session.gitModes = &gitModes{}
	result := &analysisResult{Diagnostics: []diagmodel.Diagnostic{}, FileCount: len(request.Sources), files: make([]analyzedFile, len(request.Sources))}
	sourceContents := map[string][]byte{}
	sourcePaths := make([]string, 0, len(allSources))
	for _, source := range allSources {
		path := source.InputPath
		if path == "" {
			path = source.Path
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(session.workingDir, path)
		}
		sourceContents[filepath.Clean(path)] = source.Content
		sourcePaths = append(sourcePaths, filepath.Clean(path))
	}
	session.workflowNames = &workflownames.Index{Paths: sourcePaths, Reader: reader, Load: func(path string) (string, bool, error) {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		content, ok := sourceContents[filepath.Clean(path)]
		if ok {
			reader.Record(path)
		} else {
			var err error
			content, err = reader.ReadFile(path)
			if err != nil {
				return "", false, err
			}
		}
		workflow, errs := documentparse.Parse(content)
		if workflow == nil || len(errs) > 0 {
			return "", false, nil
		}
		if workflow.Name == nil {
			return "", true, nil
		}
		if literal := exprlang.LiteralExpressionValue(workflow.Name.Value); literal != nil {
			return *literal, true, nil
		}
		return workflow.Name.Value, !workflow.Name.ContainsExpression(), nil
	}}
	// Initialize shared caches before any analysis goroutines access them.
	actionContents := make(map[string][]byte)
	for path, content := range sourceContents {
		if isActionInput(path) {
			actionContents[fsread.PhysicalPath(path)] = content
		}
	}
	for _, source := range request.Sources {
		ac, wc := actions.GetCache(projectRoot(source.Project)), workflows.GetCache(projectRoot(source.Project))
		ac.Configure(actionlocal.Options{Reader: reader, CustomReader: request.ReadFile != nil, Sources: actionContents,
			OnDocument: func(path string, meta *actionmeta.Metadata, parseErr error) {
				documents.add(documentparse.ActionOutline(path, meta, parseErr))
			},
		})
		wc.SetReader(reader)
	}
	group := errgroup.Group{}
	group.SetLimit(runtime.NumCPU())
	for i, source := range request.Sources {
		path := source.InputPath
		if path == "" {
			path = source.Path
		}
		inputs.add(path)
		ac, wc := actions.GetCache(projectRoot(source.Project)), workflows.GetCache(projectRoot(source.Project))
		group.Go(func() error {
			file := &result.files[i]
			file.source = source
			var err error
			file.errors, file.outline, err = session.check(source.Path, source.Content, source.Project, source.Config, proc, ac, wc, &file.rules)
			if err != nil {
				return &sourceAnalysisError{path: source.Path, err: err, batch: len(request.Sources) > 1}
			}
			return err
		})
	}
	err = group.Wait()
	slices.SortFunc(session.runnerReports, func(a, b runner.Report) int { return strings.Compare(a.Path, b.Path) })
	result.RunnerEvidence = session.runnerReports
	if err == nil {
		err = ctx.Err()
	}
	result.collectDiagnostics()
	result.Rules = checks.Templates()
	for _, file := range result.files {
		for _, descriptor := range file.rules {
			result.Rules = append(result.Rules, descriptor.Clone())
		}
	}
	result.Documents = make(documentmodel.Outlines, 0, len(result.files))
	explicitActions := make(map[string]bool)
	for _, file := range result.files {
		result.Documents = append(result.Documents, file.outline)
		if _, action := file.outline.(documentmodel.Action); action {
			explicitActions[fsread.PhysicalPath(file.source.InputPath)] = true
		}
	}
	for _, document := range documents.list() {
		path := diagnosticpolicy.RelativePath(session.workingDir, document.DocumentPath())
		if !explicitActions[fsread.PhysicalPath(document.DocumentPath())] {
			result.Documents = append(result.Documents, document.WithPath(path))
		}
	}
	result.Inputs = inputs.list()
	return result, err
}

type actionDocuments struct {
	mu     sync.Mutex
	byPath map[string]documentmodel.Action
}

func (d *actionDocuments) add(document documentmodel.Action) {
	d.mu.Lock()
	defer d.mu.Unlock()
	path := absPath(document.Path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	d.byPath[path] = document
}

func (d *actionDocuments) list() documentmodel.Outlines {
	d.mu.Lock()
	defer d.mu.Unlock()
	paths := make([]string, 0, len(d.byPath))
	for path := range d.byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	documents := make(documentmodel.Outlines, 0, len(paths))
	for _, path := range paths {
		documents = append(documents, d.byPath[path])
	}
	return documents
}

func (r *analysisResult) collectDiagnostics() {
	r.FileCount = len(r.files)
	files := make([]diagnosticpolicy.File, len(r.files))
	for i, file := range r.files {
		project := ""
		if file.source.Project != nil {
			project = absPath(file.source.Project.RootDir())
		}
		_, action := file.outline.(documentmodel.Action)
		files[i] = diagnosticpolicy.File{Project: project, InputPath: file.source.InputPath, Content: file.source.Content, Action: action, Findings: file.errors}
	}
	r.Diagnostics, r.Sources = diagnosticpolicy.Collect(files)
	if r.Diagnostics == nil {
		r.Diagnostics = []diagmodel.Diagnostic{}
	}
	for i, file := range files {
		r.files[i].errors = file.Findings
	}
}

type sourceAnalysisError struct {
	batch bool
	path  string
	err   error
}

func (e *sourceAnalysisError) Error() string { return e.err.Error() }
func (e *sourceAnalysisError) Unwrap() error { return e.err }

// LegacyAnalysisError preserves the root-command single-file and batch error messages.
func LegacyAnalysisError(err error) error {
	if source, ok := errors.AsType[*sourceAnalysisError](err); ok {
		if source.batch {
			return fmt.Errorf("fatal error while checking %s: %w", source.path, source.err)
		}
		return source.err
	}
	return err
}
