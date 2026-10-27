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
	"time"

	documentmodel "actionlint.kjanat.dev/v2/document"

	"actionlint.kjanat.dev/v2/analyzer"
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/checks"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflownames "actionlint.kjanat.dev/v2/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/runner"
	sourcemodel "actionlint.kjanat.dev/v2/source"

	"golang.org/x/sync/errgroup"
)

// sourceUnit is a workflow or action manifest and its resolved configuration.
type sourceUnit = workspace.Source

// analysisRequest contains resolved sources and analysis settings.
type analysisRequest struct {
	// Tests may shorten teardown without changing the production policy.
	captureJoinTimeout   time.Duration
	Runners              runner.Options
	Sources              []sourceUnit
	Analyzers            []analyzer.Registration
	Tools                []tools.Registration
	IgnorePatterns       repoconfig.IgnorePatterns
	OnRulesCreated       func([]workflowvisit.Rule) []workflowvisit.Rule
	WorkingDir           string
	ReadFile             func(string) ([]byte, error)
	RulePresets          repoconfig.Presets
	LoadScriptEngine     func(context.Context, string) (analyzer.ScriptBackend, error)
	LogWriter            io.Writer
	LogLevel             LogLevel
	Processes            *analyzerhost.ProcessPool
	AllowConfigProcesses bool
}

// analysisResult contains findings, their sources, and every local input read during analysis.
type analysisResult struct {
	report.Analysis
	files []analyzedFile
}

type analyzedFile struct {
	captured capture.Snapshot
	source   sourceUnit
	errors   []*findingmodel.Finding
	rules    []rule.Descriptor
	outline  documentmodel.Outline
}

type inputFiles struct{ workspace.Inputs }

func (f *inputFiles) add(path string) { f.Add(path) }
func (f *inputFiles) list() []string  { return f.List() }
func (f *inputFiles) addConfig(cfg *repoconfig.Config, project *workspace.Project, workingDir string) {
	f.AddConfig(cfg, project, workingDir)
}

// runAnalysis checks resolved workflows and action manifests without rendering diagnostics.
func runAnalysis(ctx context.Context, request analysisRequest) (*analysisResult, error) {
	log := request.LogWriter
	if log == nil {
		log = io.Discard
	}
	return run(ctx, request, log, request.LogLevel)
}

// sourceCaptureJoinTimeout bounds teardown of non-cooperative content/metadata IO.
// A join failure prevents publication; it never turns into a partial result.
const sourceCaptureJoinTimeout = 5 * time.Second

func joinSourceCaptures(ctx context.Context, captures *capture.Store, cancel context.CancelFunc, budget time.Duration, abort bool) (bool, error) {
	if abort {
		cancel()
	}
	if budget <= 0 {
		budget = sourceCaptureJoinTimeout
	}
	closeOnce := func() error {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), budget)
		defer stop()
		return captures.Close(cleanup)
	}
	first := closeOnce()
	if first == nil {
		return true, nil
	}
	cancel()
	retry := closeOnce()
	return retry == nil, errors.Join(first, retry)
}

func run(ctx context.Context, request analysisRequest, log io.Writer, level LogLevel) (result *analysisResult, resultErr error) {
	if request.Processes == nil {
		return nil, errors.New("analysis requires an application-owned process pool")
	}
	var extensions []rule.Descriptor
	for _, registration := range request.Analyzers {
		if analyzerhost.IsNilAnalyzer(registration.Analyzer) {
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
		if registration.ToolIntegration != nil {
			if _, registered := rulecatalog.Find(registration.Descriptor.Name); registered {
				extensions = append(extensions, registration.Descriptor)
			}
		}
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
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := fsread.New(lifetime, request.ReadFile, inputs.add)
	allSources := slices.Clone(request.Sources)
	overlays := make([]capture.Overlay, len(allSources))
	sourcePaths := make([]string, len(allSources))
	for index := range allSources {
		unit := &allSources[index]
		path := unit.InputPath
		if path == "" {
			path = unit.Path
		}
		if path != "<stdin>" && !filepath.IsAbs(path) {
			path = filepath.Join(request.WorkingDir, path)
		}
		unit.InputPath = path
		sourcePaths[index] = path
		overlays[index] = capture.Overlay{Path: path, Content: unit.Content, Coordinates: sourcemodel.YAML}
	}
	captures, err := capture.New(lifetime, overlays, func(_ context.Context, path string) ([]byte, error) {
		return reader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		return nil, err
	}
	joined := false
	join := func(abort bool) (bool, error) {
		sealed, err := joinSourceCaptures(ctx, captures, cancel, request.captureJoinTimeout, abort)
		joined = true
		return sealed, err
	}
	defer func() {
		if !joined {
			if _, err := join(true); err != nil {
				result, resultErr = nil, errors.Join(resultErr, err)
			}
		}
	}()
	snapshots, err := documentparse.NewSourceSnapshots(captures)
	if err != nil {
		return nil, err
	}
	explicitDocuments := make(map[string]struct{}, len(allSources))
	for _, unit := range allSources {
		document, err := captures.Canonical(lifetime, unit.InputPath)
		if err != nil {
			return nil, err
		}
		explicitDocuments[document] = struct{}{}
	}
	selected := make([]sourceUnit, 0, len(request.Sources))
	for _, source := range allSources {
		// Selection itself consumes configuration, even when no source survives.
		inputs.addConfig(source.Config, source.Project, request.WorkingDir)
		root := request.WorkingDir
		if source.Project != nil {
			root = source.Project.RootDir()
		}
		if source.Config.IncludesFile(source.InputPath, root) {
			selected = append(selected, source)
		}
	}
	request.Sources = selected
	if level != LogLevelNone {
		log = &analysisLogWriter{out: log}
	}
	logger := analysisLogger{log, level}
	if load := request.LoadScriptEngine; load != nil {
		request.LoadScriptEngine = func(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
			reference, local, err := pluginartifact.NormalizeSource(path, request.WorkingDir)
			if err != nil {
				return nil, err
			}
			inputs.add(local)
			return load(ctx, reference)
		}
	}
	session := &analysisSession{
		runners:   request.Runners,
		analyzers: request.Analyzers, ctx: lifetime, toolDescriptors: extensions,
		captures: captures, snapshots: snapshots, reader: reader,
		tools: request.Tools, loadScriptEngine: request.LoadScriptEngine, rulePresets: request.RulePresets, allowConfigProcesses: request.AllowConfigProcesses,
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
	session.workingDir = absPath(cwd)
	session.gitModes = &gitModes{}
	result = &analysisResult{Diagnostics: []diagmodel.Diagnostic{}, FileCount: len(request.Sources), files: make([]analyzedFile, len(request.Sources))}
	session.workflowNames = &workflownames.Index{Paths: sourcePaths, Reader: reader, Load: func(path string) (string, bool, error) {
		captured, err := captures.Read(lifetime, path, sourcemodel.YAML)
		if err != nil {
			return "", false, err
		}
		workflow, errs := documentparse.Parse(captured.Bytes())
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
	for _, source := range request.Sources {
		ac, wc := actions.GetCache(projectRoot(source.Project)), workflows.GetCache(projectRoot(source.Project))
		ac.Configure(actionlocal.Options{
			Reader: reader, CustomReader: request.ReadFile != nil, Captures: captures, Context: lifetime, WorkingDir: session.workingDir, ExplicitDocuments: explicitDocuments,
			OnDocument: func(path string, meta *actionmeta.Metadata, parseErr error) {
				captured, _, err := captures.Lookup(path)
				if err != nil {
					documents.addError(err)
					return
				}
				documents.add(captured.Revision().Document(), documentparse.ActionOutline(path, meta, parseErr))
			},
		})
		wc.SetReader(reader)
		wc.SetSources(lifetime, captures, session.workingDir)
	}
	group := errgroup.Group{}
	group.SetLimit(runtime.NumCPU())
	entryErrors := make([]error, len(request.Sources))
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
			captured, err := captures.Read(lifetime, source.InputPath, sourcemodel.YAML)
			if err == nil {
				file.captured = captured
				file.source.Content = captured.Bytes()
				file.errors, file.outline, err = session.check(source.Path, source.InputPath, captured, source.Project, source.Config, proc, ac, wc, &file.rules)
			}
			if err != nil {
				entryErrors[i] = &sourceAnalysisError{path: source.Path, err: err, batch: len(request.Sources) > 1}
			}
			return nil
		})
	}
	groupErr := group.Wait()
	err = errors.Join(append(entryErrors, groupErr, documents.error())...)
	if err == nil {
		err = ctx.Err()
	}
	sealed, joinErr := join(err != nil)
	err = errors.Join(err, joinErr)
	if !sealed {
		return nil, err
	}
	rows, sourceErr := captures.Sources()
	if sourceErr != nil {
		return nil, errors.Join(err, sourceErr)
	}
	for _, row := range rows {
		if row.State == capture.Captured || row.State == capture.Conflicted {
			for _, alias := range row.Aliases {
				inputs.add(alias)
			}
		}
	}
	err = result.revalidateCaptures(captures, rows, err)
	slices.SortFunc(session.runnerReports, func(a, b runner.Report) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return strings.Compare(a.Identity, b.Identity)
	})
	result.RunnerEvidence = session.runnerReports
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
		if file.outline != nil {
			result.Documents = append(result.Documents, file.outline)
		}
		if _, action := file.outline.(documentmodel.Action); action {
			explicitActions[file.captured.Revision().Document()] = true
		}
	}
	for _, document := range documents.list() {
		path := diagnosticpolicy.RelativePath(session.workingDir, document.DocumentPath())
		captured, _, lookupErr := captures.Lookup(document.DocumentPath())
		if lookupErr != nil {
			err = errors.Join(err, lookupErr)
			continue
		}
		if !explicitActions[captured.Revision().Document()] {
			result.Documents = append(result.Documents, document.WithPath(path))
		}
	}
	result.Inputs = inputs.list()
	return result, err
}

type actionDocuments struct {
	mu     sync.Mutex
	byPath map[string]documentmodel.Action
	err    error
}

func (d *actionDocuments) add(identity string, document documentmodel.Action) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if previous, exists := d.byPath[identity]; !exists || document.Path < previous.Path {
		d.byPath[identity] = document
	}
}

func (d *actionDocuments) addError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = errors.Join(d.err, err)
}

func (d *actionDocuments) error() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
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

// revalidateCaptures is the final publication boundary: a handle admitted before
// a later conflict cannot publish an entry, observation or entry outline.
func (r *analysisResult) revalidateCaptures(captures *capture.Store, rows []capture.Source, prior error) error {
	admitted := make(map[string]capture.Source, len(rows))
	for _, row := range rows {
		admitted[row.Document] = row
		if row.State == capture.Conflicted && !errors.Is(prior, row.Error) {
			prior = errors.Join(prior, row.Error)
		}
	}
	for index := range r.files {
		file := &r.files[index]
		if file.captured.Valid() {
			if err := captures.Verify(file.captured, file.captured.Bytes()); err != nil {
				file.errors, file.outline = nil, nil
				if !errors.Is(prior, err) {
					prior = errors.Join(prior, err)
				}
				continue
			}
		}
		accepted := file.errors[:0]
		for _, finding := range file.errors {
			if reference := finding.Reference; reference != nil {
				row, exists := admitted[reference.Document]
				if !exists || row.State != capture.Captured || row.Revision.Digest() != reference.Revision {
					if row.Error != nil {
						if !errors.Is(prior, row.Error) {
							prior = errors.Join(prior, row.Error)
						}
					} else {
						prior = errors.Join(prior, fmt.Errorf("finding source %q has no admitted captured revision", reference.Document))
					}
					continue
				}
			}
			accepted = append(accepted, finding)
		}
		clear(file.errors[len(accepted):])
		file.errors = accepted
	}
	return prior
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
