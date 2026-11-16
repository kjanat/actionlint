package checking

import (
	"bytes"
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

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/model/workflows/names"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/checking/selection"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"

	"actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/input/files"

	"actionlint.kjanat.dev/v2/internal/input/files/nameindex"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext/capture"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/services"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"

	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/results/finding"

	"actionlint.kjanat.dev/v2/internal/results/outline"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
	"golang.org/x/sync/errgroup"
)

// analysisRequest contains resolved sources and analysis settings.
type analysisRequest struct {
	ExecutionEnvironment
	RemoteActions   actions.Provider
	RemoteWorkflows reusableworkflows.Provider
	Repository      func(context.Context, string) repository.Snapshot
	// Tests may shorten teardown without changing the production policy.
	captureJoinTimeout time.Duration
	Sources            []workspace.InputFile
	SourceOverlays     []SourceOverlay
	DiskSources        bool
	NoIgnore           bool
	// Checks holds the analyzers and tools registerChecks validated, and the
	// rule set they extend.
	Checks          *registeredChecks
	IgnorePatterns  configuration.IgnorePatterns
	IgnoreSelectors policy.IgnoreSelectors
	OnRulesCreated  func([]workflowvisit.Rule) []workflowvisit.Rule
	WorkingDir      string
	// LocalDependencyRoot supplies the explicit base for prepared sources without
	// a repository identity. Empty leaves their local dependency root unknown.
	LocalDependencyRoot  string
	ReadFile             func(string) ([]byte, error)
	RulePresets          configuration.Presets
	LoadScriptEngine     func(context.Context, string) (scriptengine.Backend, error)
	LogWriter            io.Writer
	LogLevel             LogLevel
	Processes            *hostprocess.ProcessPool
	AllowConfigProcesses bool
}

// analysisResult contains findings, their sources, and every local input read during analysis.
type analysisResult struct {
	report.Analysis
	files []analyzedFile
}

type analyzedFile struct {
	captured    capture.CapturedDocument
	source      workspace.InputFile
	errors      []*finding.Finding
	publication *policy.Batch
	rules       []rule.Descriptor
	outline     outline.Outline
}

type inputFiles struct{ workspace.InputFiles }

func (f *inputFiles) add(path string) { f.Add(path) }
func (f *inputFiles) list() []string  { return f.List() }
func (f *inputFiles) addConfig(cfg *configuration.Config, provenance configuration.Provenance, project *workspace.Project, workingDir string) {
	f.AddConfig(cfg, provenance, project, workingDir)
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

type sourceCaptureInitialization interface {
	Join(context.Context) error
}

type sourceCaptureInitFailure interface {
	error
	sourceCaptureInitialization
	Pending() bool
}

var _ sourceCaptureInitFailure = (*capture.InitError)(nil)

type sourceCaptureConstructor func(context.Context, []capture.Overlay, capture.Reader) (*capture.Store, error)

func joinSourceCaptureInitialization(ctx context.Context, cause error, initialization sourceCaptureInitialization, cancel context.CancelFunc, budget time.Duration) error {
	cancel()
	if budget <= 0 {
		budget = sourceCaptureJoinTimeout
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer stop()
	return errors.Join(cause, initialization.Join(cleanup))
}

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

func run(ctx context.Context, request analysisRequest, log io.Writer, level LogLevel) (*analysisResult, error) {
	return runWithSourceCaptureConstructor(ctx, request, log, level, capture.New)
}

func runWithSourceCaptureConstructor(ctx context.Context, request analysisRequest, log io.Writer, level LogLevel, newCaptures sourceCaptureConstructor) (result *analysisResult, resultErr error) {
	request.ExecutionEnvironment = request.captureDate(time.Now)
	if request.Processes == nil {
		return nil, errors.New("analysis requires an application-owned process pool")
	}
	if request.Checks == nil {
		return nil, errors.New("analysis requires registered checks")
	}
	for _, tool := range request.Checks.tools {
		if tool.Build == nil {
			return nil, fmt.Errorf("tool %q has no adapter", tool.Name)
		}
		if tool.Validate != nil {
			if err := tool.Validate(); err != nil {
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
	workingDir, err := filepath.Abs(request.WorkingDir)
	if err != nil {
		return nil, err
	}
	request.WorkingDir = workingDir
	inputs := &inputFiles{}
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := files.New(lifetime, request.ReadFile, inputs.add)
	contentReader := files.New(lifetime, request.ReadFile, nil)
	allSources := slices.Clone(request.Sources)
	overlays := make([]capture.Overlay, 0, len(allSources)+len(request.SourceOverlays))
	for _, overlay := range request.SourceOverlays {
		path := overlay.Path
		if path != "<stdin>" && !filepath.IsAbs(path) {
			path = filepath.Join(request.WorkingDir, path)
		}
		if path != "<stdin>" {
			path = filepath.Clean(path)
		}
		overlays = append(overlays, capture.Overlay{Path: path, Content: overlay.Content, Coordinates: sourcetext.YAML})
	}
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
		if !request.DiskSources {
			overlays = append(overlays, capture.Overlay{Path: path, Content: unit.Content, Coordinates: sourcetext.YAML})
		}
	}
	// The capture reader reuses selected file contents from allSources.
	// Explicit editor overlays supply their contents through overlays.
	selectedDocuments := make(map[string]int, len(allSources))
	captures, err := newCaptures(lifetime, overlays, func(_ context.Context, path string) ([]byte, error) {
		if index, selected := selectedDocuments[path]; selected {
			return allSources[index].Content, nil
		}
		return contentReader.ReadFile(filepath.FromSlash(path))
	})
	if err != nil {
		cancel()
		if initialization, ok := errors.AsType[sourceCaptureInitFailure](err); ok && initialization.Pending() {
			return nil, joinSourceCaptureInitialization(ctx, err, initialization, cancel, request.captureJoinTimeout)
		}
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
	canonicalWorkingDir, err := captures.CanonicalIdentity(lifetime, request.WorkingDir)
	if err != nil {
		return nil, err
	}
	snapshots, err := parsing.NewSourceSnapshots(captures)
	if err != nil {
		return nil, err
	}
	explicitDocuments := make(map[string]struct{}, len(overlays))
	for _, overlay := range overlays {
		document, err := captures.Canonical(lifetime, overlay.Path)
		if err != nil {
			return nil, err
		}
		explicitDocuments[document] = struct{}{}
	}
	if request.DiskSources {
		for index, unit := range allSources {
			document, err := captures.Canonical(lifetime, unit.InputPath)
			if err != nil {
				return nil, err
			}
			previous, exists := selectedDocuments[document]
			if !exists || unit.InputPath < allSources[previous].InputPath || unit.InputPath == allSources[previous].InputPath && bytes.Compare(unit.Content, allSources[previous].Content) < 0 {
				selectedDocuments[document] = index
			}
		}
	}
	selected := make([]workspace.InputFile, 0, len(request.Sources))
	for _, source := range allSources {
		if source.ConfigProvenance.Path() == "" && source.Project != nil && source.Config == source.Project.Config() {
			source.ConfigProvenance = source.Project.ConfigProvenance()
		}
		// Selection itself consumes configuration, even when no source survives.
		inputs.addConfig(source.Config, source.ConfigProvenance, source.Project, request.WorkingDir)
		root := request.WorkingDir
		if source.Project != nil {
			root = source.Project.RootDir()
		}
		if source.Config.IncludesSelectedFile(source.InputPath, root, request.NoIgnore || source.ExplicitFile) {
			selected = append(selected, source)
		}
	}
	request.Sources = selected
	if level != LogLevelNone {
		log = &analysisLogWriter{out: log}
	}
	logger := analysisLogger{log, level}
	if load := request.LoadScriptEngine; load != nil {
		request.LoadScriptEngine = func(ctx context.Context, path string) (scriptengine.Backend, error) {
			reference, local, err := pluginreference.NormalizeSource(path, request.WorkingDir)
			if err != nil {
				return nil, err
			}
			inputs.add(local)
			return load(ctx, reference)
		}
	}
	session := &analysisSession{
		remoteActions: make(map[references.MirrorAccess]*remote.Service),
		environment:   request.ExecutionEnvironment,
		checks:        request.Checks, ctx: lifetime,
		captures: captures, snapshots: snapshots, reader: reader,
		loadScriptEngine: request.LoadScriptEngine, rulePresets: request.RulePresets, allowConfigProcesses: request.AllowConfigProcesses,
		ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, analysisLogger: logger,
		ignoreSelectors: request.IgnoreSelectors,
	}

	documents := &actionDocuments{byPath: map[string]outline.Action{}}
	session.actionDocuments = documents
	proc := request.Processes
	localActionFactory := localactions.NewFactory(session.debugWriter())
	cwd := request.WorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	// Local dependencies use the project root or an explicitly supplied dependency root.
	type workflowCacheKey struct {
		root    string
		mirrors references.MirrorAccess
	}
	workflows := map[workflowCacheKey]*reusableworkflows.Cache{}
	remoteWorkflows := make(map[references.MirrorAccess]*reusableworkflows.Remote)
	session.workingDir = absPath(cwd)
	session.repositories = make(map[string]repository.Snapshot)
	for _, source := range request.Sources {
		root := projectRoot(source.Project)
		if _, exists := session.repositories[root]; exists {
			continue
		}
		var facts repository.Snapshot
		if request.Repository != nil && root != "" {
			facts = request.Repository(lifetime, root)
		}
		if facts.Service.Kind() == services.Unknown {
			facts.Service = request.Service
		}
		session.repositories[root] = facts
	}
	session.gitModes = &gitModes{}
	result = &analysisResult{Diagnostics: []results.PublishedDiagnostic{}, FileCount: len(request.Sources), files: make([]analyzedFile, len(request.Sources))}
	session.workflowNames = nameindex.NewIndex(sourcePaths, reader, func(path string) (names.Declared, error) {
		captured, err := captures.Read(lifetime, path, sourcetext.YAML)
		if err != nil {
			return names.Unknown(), err
		}
		workflow, errs := parser.ParseWorkflow(captured.Bytes())
		if workflow == nil || len(errs) > 0 {
			return names.Unknown(), nil
		}
		return names.Declare(workflow.Name), nil
	})
	// Initialize shared caches before any analysis goroutines access them.
	for _, source := range request.Sources {
		root := dependencyRoot(source.Project, request.LocalDependencyRoot)
		mirrors := source.Config.MetadataMirrorAccess()
		if _, exists := session.remoteActions[mirrors]; !exists {
			actionProvider := request.RemoteActions
			if configurable, ok := actionProvider.(interface {
				WithMetadataMirrors(references.MirrorAccess, []string) actions.Provider
			}); ok {
				actionProvider = configurable.WithMetadataMirrors(mirrors, source.Config.MetadataMirrorExclusions())
			}
			workflowProvider := request.RemoteWorkflows
			if configurable, ok := workflowProvider.(interface {
				WithWorkflowMetadataMirrors(references.MirrorAccess, []string) reusableworkflows.Provider
			}); ok {
				workflowProvider = configurable.WithWorkflowMetadataMirrors(mirrors, source.Config.MetadataMirrorExclusions())
			}
			session.remoteActions[mirrors] = remote.New(actionProvider)
			remoteWorkflows[mirrors] = reusableworkflows.NewRemote(workflowProvider)
		}
		key := workflowCacheKey{root, mirrors}
		wc, configured := workflows[key]
		if !configured {
			wc = reusableworkflows.NewCache(root, session.debugWriter())
			wc.SetRemote(remoteWorkflows[mirrors])
			workflows[key] = wc
		}
		ac := localActionFactory.GetCache(lifetime, root)
		ac.Configure(localactions.Options{
			Reader: reader, CustomReader: request.ReadFile != nil, Captures: captures, Context: lifetime, WorkingDir: session.workingDir, ExplicitDocuments: explicitDocuments,
			OnManifestRequest: documents.request,
			OnDocument: func(captured capture.CapturedDocument, path string, meta *actionmeta.Metadata, parseErr error) {
				if err := captures.Verify(captured, captured.Bytes()); err != nil {
					documents.addError(err)
					return
				}
				documents.add(captured.Revision().Document(), outline.FromAction(path, meta, parseErr))
				if parseErr != nil || meta == nil {
					revision := captured.Revision()
					session.checkCompletion.markIncomplete(sourcetext.Reference{Document: revision.Document(), Revision: revision.Digest()})
				}
			},
		})
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
		root := dependencyRoot(source.Project, request.LocalDependencyRoot)
		ac, wc := localActionFactory.GetCache(lifetime, root), workflows[workflowCacheKey{root, source.Config.MetadataMirrorAccess()}]
		group.Go(func() error {
			file := &result.files[i]
			file.source = source
			captured, err := captures.Read(lifetime, source.InputPath, sourcetext.YAML)
			if err == nil {
				file.captured = captured
				file.source.Content = captured.Bytes()
				file.errors, file.outline, file.publication, err = session.check(source.Path, source.InputPath, captured, source.Project, source.Config, source.ConfigProvenance, proc, ac, wc, &file.rules)
			}
			if err != nil {
				if revision := file.captured.Revision(); revision.Valid() {
					session.checkCompletion.markIncomplete(sourcetext.Reference{Document: revision.Document(), Revision: revision.Digest()})
				}
				entryErrors[i] = &sourceAnalysisError{path: source.Path, err: err, batch: len(request.Sources) > 1}
			}
			return nil
		})
	}
	groupErr := group.Wait()
	err = errors.Join(errors.Join(entryErrors...), groupErr, documents.error())
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
	result.CapturedSources = make(map[string]report.CapturedSource)
	for _, row := range rows {
		if row.State != capture.Captured {
			continue
		}
		for _, alias := range row.Aliases {
			captured, state, lookupErr := captures.Lookup(alias)
			if lookupErr != nil || state != capture.Captured {
				return nil, errors.Join(err, lookupErr, errors.New("sealed source capture is unavailable"))
			}
			result.CapturedSources[alias] = report.CapturedSource{Revision: row.Revision, Coordinates: row.Coordinates, CoordinatesAvailable: row.CoordinatesAvailable, Content: captured.Bytes()}
		}
	}
	readAliases, sourceErr := captures.ReadAliases()
	if sourceErr != nil {
		return nil, errors.Join(err, sourceErr)
	}
	for _, alias := range readAliases {
		inputs.add(alias)
	}
	if completionErr := documents.complete(captures, readAliases); completionErr != nil {
		err = errors.Join(err, completionErr)
	}
	result.IncompleteChecks = session.checkCompletion.incompleteChecks()
	err = result.revalidateCaptures(captures, rows, filepath.FromSlash(canonicalWorkingDir), err)
	slices.SortFunc(session.runnerReports, func(a, b runnerapi.Report) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return strings.Compare(a.CacheKey, b.CacheKey)
	})
	result.RunnerResults = session.runnerReports
	err = errors.Join(err, result.publishDiagnostics())
	result.Rules = selection.Templates(ConfigurationRules())
	for _, file := range result.files {
		for _, descriptor := range file.rules {
			result.Rules = append(result.Rules, descriptor.Clone())
		}
	}
	for i := range result.Rules {
		result.Rules[i] = session.checks.rules.WithDefaults(result.Rules[i])
	}
	result.Documents = make(outline.Outlines, 0, len(result.files))
	explicitActions := make(map[string]bool)
	for _, file := range result.files {
		if file.outline != nil {
			result.Documents = append(result.Documents, file.outline)
		}
		if _, action := file.outline.(outline.Action); action {
			explicitActions[file.captured.Revision().Document()] = true
		}
	}
	sourceStates := make(map[string]capture.State, len(rows))
	for _, row := range rows {
		sourceStates[row.Document] = row.State
	}
	for _, document := range documents.list() {
		path := document.DocumentPath()
		if filepath.IsAbs(path) {
			if relative, relErr := filepath.Rel(session.workingDir, path); relErr == nil {
				path = relative
			}
		}
		if sourceStates[document.identity] != capture.Captured {
			continue
		}
		if !explicitActions[document.identity] {
			result.Documents = append(result.Documents, document.WithPath(path))
		}
	}
	result.Inputs = inputs.list()
	for _, mirrors := range []references.MirrorAccess{references.PublicMirrorsOnly, references.AllowUnknownMirrors, references.NoMirrors} {
		result.ActionMetadata = append(result.ActionMetadata, session.remoteActions[mirrors].Coverage()...)
	}
	return result, err
}

type actionDocuments struct {
	mu       sync.Mutex
	byPath   map[string]outline.Action
	requests map[string]struct{}
	// remote holds the remote action manifests composition parsed, by
	// captured document identity, so complete need not parse them again.
	remote map[string]parsing.Document
	err    error
}

type actionDocument struct {
	outline.Action
	identity string
}

func (d *actionDocuments) request(address string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.requests == nil {
		d.requests = make(map[string]struct{})
	}
	d.requests[address] = struct{}{}
}

// parsed records a remote action manifest composition parsed from captured.
// It is safe to call on a nil receiver, which records nothing.
func (d *actionDocuments) parsed(captured capture.CapturedDocument, document parsing.Document) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	identity := captured.Revision().Document()
	if _, exists := d.remote[identity]; exists {
		return
	}
	if d.remote == nil {
		d.remote = make(map[string]parsing.Document)
	}
	d.remote[identity] = document
}

// complete outlines consumed manifests from sealed bytes; it never resumes
// analysis. It reuses the document composition parsed for a remote manifest
// and parses only a manifest analysis captured but never parsed.
func (d *actionDocuments) complete(captures *capture.Store, consumed []string) error {
	if _, err := captures.Sources(); err != nil {
		return err
	}
	for _, address := range consumed {
		d.mu.Lock()
		_, requested := d.requests[address]
		d.mu.Unlock()
		if !requested && (!sourcetext.RemoteAddress(address) || !isActionInput(address)) {
			continue
		}
		snapshot, state, err := captures.Lookup(address)
		if err != nil || state != capture.Captured || snapshot.Coordinates() != sourcetext.YAML {
			continue
		}
		identity := snapshot.Revision().Document()
		d.mu.Lock()
		_, exists := d.byPath[identity]
		document, parsed := d.remote[identity]
		d.mu.Unlock()
		if exists {
			continue
		}
		if !parsed {
			// Analysis stopped after capturing this manifest but before
			// parsing it, so its sealed bytes are parsed here once.
			document = parsing.Action(address, snapshot.Bytes())
		}
		d.add(identity, outline.FromAction(address, document.Metadata, document.ParseError))
	}
	return nil
}

func (d *actionDocuments) add(identity string, document outline.Action) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if previous, exists := d.byPath[identity]; !exists || document.DocumentPath() < previous.DocumentPath() {
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

func (d *actionDocuments) list() []actionDocument {
	d.mu.Lock()
	defer d.mu.Unlock()
	paths := make([]string, 0, len(d.byPath))
	for path := range d.byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	documents := make([]actionDocument, 0, len(paths))
	for _, path := range paths {
		documents = append(documents, actionDocument{Action: d.byPath[path], identity: path})
	}
	return documents
}

func (r *analysisResult) publishDiagnostics() error {
	r.FileCount = len(r.files)
	batches := make([]policy.Batch, len(r.files))
	for i, file := range r.files {
		var batch policy.Batch
		if file.publication != nil {
			batch = *file.publication
		}
		batch.Project = projectRoot(file.source.Project)
		batch.InputPath, batch.Source = file.source.InputPath, file.captured.Revision()
		_, batch.Action = file.outline.(outline.Action)
		batch.Findings = file.errors
		batches[i] = batch
	}
	published, err := policy.Publish(batches, r.IncompleteChecks)
	r.Diagnostics = published.Diagnostics
	r.IncompleteChecks = published.IncompleteChecks
	if r.Diagnostics == nil {
		r.Diagnostics = []results.PublishedDiagnostic{}
	}
	for i, findings := range published.Findings {
		r.files[i].errors = findings
	}
	return err
}

// revalidateCaptures is the final publication boundary: a handle admitted before
// a later conflict cannot publish an entry, observation or entry outline.
func (r *analysisResult) revalidateCaptures(captures *capture.Store, rows []capture.TableRow, workingDir string, prior error) error {
	admitted := make(map[string]capture.TableRow, len(rows))
	withheld := 0
	withheldRelated := 0
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
				withheld += len(file.errors)
				file.errors, file.outline, file.publication = nil, nil, nil
				if !errors.Is(prior, err) {
					prior = errors.Join(prior, err)
				}
				continue
			}
		}
		if batch := file.publication; batch != nil {
			for path, index := range batch.References {
				row := admitted[index.Document.Document]
				if row.State != capture.Captured || row.Revision.Digest() != index.Document.Revision {
					delete(batch.References, path)
					delete(batch.Sources, path)
				}
			}
			for path := range batch.Sources {
				if _, exists := batch.References[path]; !exists {
					delete(batch.Sources, path)
				}
			}
		}
		accepted := file.errors[:0]
		for _, finding := range file.errors {
			if marker := finding.Source.Unavailable; marker.Present() {
				row := admitted[marker.Document()]
				if marker.Document() == "" || row.State == capture.Conflicted {
					withheld++
					if row.Error != nil && !errors.Is(prior, row.Error) {
						prior = errors.Join(prior, row.Error)
					}
					continue
				}
				// Another consumer's captured row never promotes a tool's unavailable
				// reported source. E7/M07 remove this transitional allowance.
				finding.Reference, finding.Fixes = nil, nil
				var count int
				finding.Related, count = admitRelated(finding.Related, captures, admitted, workingDir)
				withheldRelated += count
				accepted = append(accepted, finding)
				continue
			}
			entry := admitted[file.captured.Revision().Document()]
			own := finding.Path == "" || finding.Path == file.source.Path || finding.Path == file.source.InputPath || finding.Path == entry.Document
			if relative, err := filepath.Rel(workingDir, entry.Document); err == nil {
				own = own || finding.Path == relative
			}
			for _, alias := range entry.Aliases {
				own = own || finding.Path == alias
				if relative, err := filepath.Rel(workingDir, alias); err == nil {
					own = own || finding.Path == relative
				}
			}
			if !own && (finding.Reference == nil || finding.Reference.Document == file.captured.Revision().Document()) {
				withheld++
				continue
			}
			if target := finding.MetadataParseSource; target.Valid() {
				row, exists := admitted[target.Document()]
				if !exists || row.State != capture.Captured || row.Revision != target {
					withheld++
					continue
				}
			}
			if reference := finding.Reference; reference != nil {
				row, exists := admitted[reference.Document]
				if !exists || row.State != capture.Captured || row.Revision.Digest() != reference.Revision {
					withheld++
					if row.Error != nil {
						if !errors.Is(prior, row.Error) {
							prior = errors.Join(prior, row.Error)
						}
					} else {
						prior = errors.Join(prior, fmt.Errorf("finding source %q has no admitted captured revision", reference.Document))
					}
					continue
				}
			} else if finding.Source.Content != nil || finding.Source.RawScript {
				if !own || !file.captured.Valid() || captures.Verify(file.captured, finding.Source.Content) != nil {
					withheld++
					continue
				}
			}
			var count int
			finding.Related, count = admitRelated(finding.Related, captures, admitted, workingDir)
			withheldRelated += count
			accepted = append(accepted, finding)
		}
		clear(file.errors[len(accepted):])
		file.errors = accepted
	}
	if withheld != 0 {
		prior = errors.Join(prior, &admissionRejectedError{count: withheld})
	}
	return results.ConsolidateRejectedRelated(prior, withheldRelated)
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
