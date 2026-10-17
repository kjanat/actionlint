package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sync"

	"actionlint.kjanat.dev/internal/workflow/calls"

	"actionlint.kjanat.dev/internal/report"
	"actionlint.kjanat.dev/internal/workspace"

	"errors"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/internal/tools"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	"actionlint.kjanat.dev/rule"
	"golang.org/x/sync/errgroup"
)

// SourceUnit is a workflow or action manifest and its resolved configuration.
type SourceUnit = workspace.Source

// Request contains resolved sources and analysis settings.
type Request struct {
	Sources          []SourceUnit
	Analyzers        []analyzer.Registration
	Tools            []tools.Registration
	IgnorePatterns   IgnorePatterns
	OnRulesCreated   func([]Rule) []Rule
	WorkingDir       string
	ReadFile         func(string) ([]byte, error)
	RulePresets      RulePresets
	LoadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
	LogWriter        io.Writer
	LogLevel         LogLevel
	Processes        *analyzerhost.ProcessPool
}

// AnalysisResult contains findings, their sources, and every local input read during analysis.
type AnalysisResult struct {
	report.Analysis
	files []analyzedFile
}

type analyzedFile struct {
	source  SourceUnit
	errors  []*Error
	rules   []RuleInfo
	outline DocumentOutline
}

type inputFiles struct{ workspace.Inputs }

func (f *inputFiles) add(path string)                         { f.Add(path) }
func (f *inputFiles) list() []string                          { return f.List() }
func (f *inputFiles) addConfig(cfg *Config, project *Project) { f.AddConfig(cfg, project) }

// Run checks resolved workflows and action manifests without rendering diagnostics.
// On analysis failure, a non-nil result retains findings collected before the failure.
func Run(ctx context.Context, request Request) (*AnalysisResult, error) {
	log := request.LogWriter
	if log == nil {
		log = io.Discard
	}
	return run(ctx, request, log, request.LogLevel)
}

func run(ctx context.Context, request Request, log io.Writer, level LogLevel) (*AnalysisResult, error) {
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
		behaviour := ruleBehaviours()[name]
		if behaviour.build != nil || behaviour.buildScripts != nil {
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
	selected := make([]SourceUnit, 0, len(request.Sources))
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
	engine := &analysisEngine{
		analyzers: request.Analyzers, ctx: ctx, toolDescriptors: extensions,
		buildRules: registeredFactory(request, logger), rulePresets: request.RulePresets,
		ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, analysisLogger: logger,
	}

	documents := &actionDocuments{byPath: map[string]ActionOutline{}}
	proc := request.Processes
	actions := NewLocalActionsCacheFactory(engine.debugWriter())
	cwd := request.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	workflows := calls.NewCacheFactory(cwd, engine.debugWriter())
	reader := fsread.New(ctx, request.ReadFile, inputs.add)
	engine.workingDir, engine.reader = absPath(cwd), reader
	engine.gitModes = &gitModes{}
	result := &AnalysisResult{Diagnostics: []Diagnostic{}, FileCount: len(request.Sources), files: make([]analyzedFile, len(request.Sources))}
	sourceContents := map[string][]byte{}
	sourcePaths := make([]string, 0, len(allSources))
	for _, source := range allSources {
		path := source.InputPath
		if path == "" {
			path = source.Path
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(engine.workingDir, path)
		}
		sourceContents[filepath.Clean(path)] = source.Content
		sourcePaths = append(sourcePaths, filepath.Clean(path))
	}
	engine.workflowNames = &workflownames.Index{Paths: sourcePaths, Reader: reader, Load: func(path string) (string, bool, error) {
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
		workflow, errs := Parse(content)
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
			actionContents[physicalSourcePath(path)] = content
		}
	}
	for _, source := range request.Sources {
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(projectRoot(source.Project))
		ac.reader = reader
		wc.SetReader(reader)
		ac.onDocument = documents.add
		ac.customReader = request.ReadFile != nil
		ac.explicitSources = actionContents
	}
	group := errgroup.Group{}
	group.SetLimit(runtime.NumCPU())
	for i, source := range request.Sources {
		path := source.InputPath
		if path == "" {
			path = source.Path
		}
		inputs.add(path)
		ac, wc := actions.GetCache(source.Project), workflows.GetCache(projectRoot(source.Project))
		group.Go(func() error {
			file := &result.files[i]
			file.source = source
			var err error
			file.errors, file.outline, err = engine.check(source.Path, source.Content, source.Project, source.Config, proc, ac, wc, &file.rules)
			if err != nil {
				return &sourceAnalysisError{path: source.Path, err: err, batch: len(request.Sources) > 1}
			}
			return err
		})
	}
	err = group.Wait()
	if err == nil {
		err = ctx.Err()
	}
	result.collectDiagnostics()
	result.Rules = templateRules()
	for _, file := range result.files {
		for _, descriptor := range file.rules {
			result.Rules = append(result.Rules, descriptor.Clone())
		}
	}
	result.Documents = make(DocumentOutlines, 0, len(result.files))
	explicitActions := make(map[string]bool)
	for _, file := range result.files {
		result.Documents = append(result.Documents, file.outline)
		if _, action := file.outline.(ActionOutline); action {
			explicitActions[physicalSourcePath(file.source.InputPath)] = true
		}
	}
	for _, document := range documents.list() {
		path := relativeAnalysisPath(engine.workingDir, document.DocumentPath())
		if !explicitActions[physicalSourcePath(document.DocumentPath())] {
			result.Documents = append(result.Documents, document.WithPath(path))
		}
	}
	result.Inputs = inputs.list()
	return result, err
}

type actionDocuments struct {
	mu     sync.Mutex
	byPath map[string]ActionOutline
}

func (d *actionDocuments) add(document ActionOutline) {
	d.mu.Lock()
	defer d.mu.Unlock()
	path := absPath(document.Path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	d.byPath[path] = document
}

func (d *actionDocuments) list() DocumentOutlines {
	d.mu.Lock()
	defer d.mu.Unlock()
	paths := make([]string, 0, len(d.byPath))
	for path := range d.byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	documents := make(DocumentOutlines, 0, len(paths))
	for _, path := range paths {
		documents = append(documents, d.byPath[path])
	}
	return documents
}

func (r *AnalysisResult) collectDiagnostics() {
	r.FileCount = len(r.files)
	type location struct {
		project, path, rule string
		start               DiagnosticPosition
	}
	seen := make(map[location][]Diagnostic)
	type metadataLocation struct {
		project, kind, severity string
		finding                 actionMetadataFindingKey
	}
	seenMetadata := make(map[metadataLocation]bool)
	type declarationLocation struct {
		project string
		finding actionMetadataFindingKey
	}
	declarations := make(map[declarationLocation]bool)
	parseDeclarations := make(map[string]bool)
	for _, file := range r.files {
		project := ""
		if file.source.Project != nil {
			project = absPath(file.source.Project.RootDir())
		}
		for _, finding := range file.errors {
			if _, action := file.outline.(ActionOutline); action && finding.Rule == "syntax-check" {
				parseDeclarations[physicalSourcePath(file.source.InputPath)] = true
			}
			if identity := finding.metadataFinding; identity != nil && identity.declaration && finding.Rule == "action" {
				declarations[declarationLocation{project, identity.key}] = true
			}
		}
	}
	for i := range r.files {
		file := &r.files[i]
		project := ""
		if file.source.Project != nil {
			project = absPath(file.source.Project.RootDir())
		}
		unique := file.errors[:0]
		for _, finding := range file.errors {
			if finding.Rule == "action" && finding.metadataParsePath != "" && parseDeclarations[physicalSourcePath(finding.metadataParsePath)] {
				continue
			}
			diagnostic := finding.diagnostic(file.source.Content)
			identityPath := file.source.InputPath
			if finding.source != nil {
				identityPath = diagnostic.Path
			}
			completeDiagnostic(&diagnostic, finding.sourceFor(file.source.Content), identityPath)
			if identity := finding.metadataFinding; identity != nil && finding.Rule == "action" {
				key := metadataLocation{project, finding.Rule, finding.Severity, identity.key}
				if !identity.declaration && declarations[declarationLocation{project, identity.key}] {
					continue
				}
				if seenMetadata[key] {
					continue
				}
				seenMetadata[key] = true
			}
			key := location{project, diagnostic.Path, diagnostic.Rule, diagnostic.Start}
			if slices.ContainsFunc(seen[key], func(previous Diagnostic) bool {
				return reflect.DeepEqual(previous, diagnostic)
			}) {
				continue
			}
			seen[key] = append(seen[key], diagnostic)
			r.Diagnostics = append(r.Diagnostics, diagnostic)
			r.Sources = append(r.Sources, report.DiagnosticSource{Content: finding.sourceFor(file.source.Content), RawScript: finding.rawScriptSource})
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
