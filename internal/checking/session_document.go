package checking

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking/binding"

	runnerinput "actionlint.kjanat.dev/v2/internal/input/runners"

	"actionlint.kjanat.dev/v2/internal/checking/executor"

	composites "actionlint.kjanat.dev/v2/internal/checking/executor/composites"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/checking/selection"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	workflownames "actionlint.kjanat.dev/v2/internal/input/files/names"
	"actionlint.kjanat.dev/v2/internal/input/references"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	documentmodel "actionlint.kjanat.dev/v2/internal/results/outline"
	resultpolicy "actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
	"golang.org/x/sync/errgroup"
)

const concurrentMetadataLookups = 8

// analysisSession holds invocation resources assembled by the application.
// The workflow engine receives only prepared passes from the composition service.
type analysisSession struct {
	checkCompletion checkCompletion
	runnerReportsMu sync.Mutex
	runnerReports   []runner.Report
	environment     ExecutionEnvironment

	ruleRegistrations    []configuration.RuleRegistration
	rules                configuration.RuleSet
	tools                []tools.Registration
	analyzers            []checkinputs.Registration
	loadScriptEngine     func(context.Context, string) (checkinputs.ScriptBackend, error)
	allowConfigProcesses bool
	workflowNames        *workflownames.Index
	remoteActions        map[references.MirrorAccess]*actionremote.Service
	repositories         map[string]repository.Snapshot
	analysisLogger
	ctx             context.Context
	workingDir      string
	reader          *files.Reader
	captures        *capture.Store
	snapshots       *parsing.SourceSnapshots
	gitModes        *gitModes
	ignorePats      configuration.IgnorePatterns
	ignoreSelectors resultpolicy.IgnoreSelectors
	onRulesCreated  func([]workflowvisit.Rule) []workflowvisit.Rule
	rulePresets     configuration.Presets
}

// check parses action manifests and workflows with their respective parsers.
func (s *analysisSession) check(path, sourcePath string, captured capture.CapturedDocument, project *workspace.Project, config *configuration.Config, provenance configuration.Provenance,
	processes *hostprocess.ProcessPool, actions *workspaceactions.Cache, workflows *workflowinputs.Cache, usedRules *[]rulemeta.Descriptor,
) ([]*findingmodel.Finding, documentmodel.Outline, *resultpolicy.Batch, error) {
	if s.captures == nil || s.snapshots == nil {
		return nil, nil, nil, errors.New("analysis requires result-owned source captures")
	}
	content := captured.Bytes()
	if err := s.captures.Verify(captured, content); err != nil {
		return nil, nil, nil, err
	}
	if isActionInput(sourcePath) {
		document := parsing.Action(sourcePath, content)
		return s.analyzeDocument(path, sourcePath, captured, content, document, true, project, config, provenance, processes, actions, workflows, usedRules)
	}
	document := parsing.Workflow(content)
	return s.analyzeDocument(path, sourcePath, captured, content, document, false, project, config, provenance, processes, actions, workflows, usedRules)
}

func (s *analysisSession) analyzeDocument(path, sourcePath string, captured capture.CapturedDocument, content []byte, document parsing.Document, action bool,
	project *workspace.Project, projectConfig *configuration.Config, provenance configuration.Provenance, processes *hostprocess.ProcessPool,
	actions *workspaceactions.Cache, workflows *workflowinputs.Cache, usedRules *[]rulemeta.Descriptor,
) (findings []*findingmodel.Finding, outline documentmodel.Outline, publication *resultpolicy.Batch, resultErr error) {
	outline = documentOutline(path, document)
	root := s.workingDir
	if project != nil {
		root = project.RootDir()
	}
	canonicalRoot, err := s.captures.CanonicalIdentity(s.ctx, absPath(root))
	if err != nil {
		return nil, outline, nil, err
	}
	canonicalWorkingDir, err := s.captures.CanonicalIdentity(s.ctx, s.workingDir)
	if err != nil {
		return nil, outline, nil, err
	}
	settings := newRuleConfigurations(projectConfig, root, s.rulePresets, s.rules, s.ruleRegistrations)
	var cfg *configuration.Config
	if action {
		cfg, err = settings.action(sourcePath)
	} else {
		cfg, err = settings.workflow(sourcePath)
	}
	if err != nil {
		return nil, outline, nil, err
	}
	entryBinding := settings.bindEntry(sourcePath, cfg)
	names := selection.RuleNames(s.analyzers)
	for _, registration := range s.ruleRegistrations {
		names = append(names, registration.Descriptor.Name)
	}
	if err := settings.rules.ValidateRuleNames(cfg, names...); err != nil {
		return nil, nil, nil, err
	}
	if err := configuration.ValidateToolNames(projectConfig, toolNames(s.tools)...); err != nil {
		return nil, nil, nil, err
	}
	registrations, err := selection.ConfigureAnalyzers(entryBinding, s.analyzers)
	if err != nil {
		return nil, nil, nil, err
	}
	start := time.Now()
	s.log("Linting", path)
	if project != nil {
		s.log("Using project at", project.RootDir())
	}
	if cfg != nil {
		s.debug("Config: %#v", cfg)
	} else {
		s.debug("No config was found")
	}
	s.log("Found", len(document.SyntaxErrors), "parse errors in", time.Since(start).Milliseconds(), "ms for", path)

	all := correctness.SyntaxCheckFindings(document.SyntaxErrors)
	var analysisErr error
	snapshot, err := s.snapshots.Document(captured, document)
	if err != nil {
		return all, outline, nil, err
	}
	if !document.CanAnalyze() || len(document.SyntaxErrors) != 0 || document.ParseError != nil {
		s.checkCompletion.markIncomplete(snapshot.Reference())
	}
	// Resolve the requested path before sealing the captures. This records
	// metadata path aliases, including Windows drive spellings, for later lookups.
	if path != "" {
		displayAddress := path
		if path != "<stdin>" && !filepath.IsAbs(path) {
			displayAddress = filepath.Join(s.workingDir, path)
		}
		if _, err := s.captures.Canonical(s.ctx, displayAddress); err != nil {
			return all, outline, nil, err
		}
	}
	composed := composites.Result{EntrySnapshot: snapshot, Sources: map[string][]byte{}}
	if document.CanAnalyze() {
		composed, analysisErr = s.composeDocument(path, sourcePath, root, content, document, snapshot, action, project, cfg, projectConfig, settings, provenance, processes, actions, workflows, registrations, usedRules)
		if composed.Sources == nil {
			return nil, outline, nil, analysisErr
		}
		all = append(all, composed.Findings...)
	}
	references := map[string]workflowfacts.SourceIndex{sourcePath: snapshot.CanonicalReferences()}
	defer func() {
		if resultErr != nil {
			invocations := indexCheckInvocations(composed.Invocations)
			for _, index := range references {
				for _, context := range invocations.forSource(index.Document) {
					s.checkCompletion.markIncompleteInvocation(index.Document, context)
				}
			}
		}
	}()
	rejected, sourceErr := s.admitSourceDocuments(composed, references)
	analysisErr = errors.Join(analysisErr, sourceErr)
	for path := range composed.Sources {
		if _, err := settings.bindSource(path); err != nil {
			return all, outline, nil, errors.Join(analysisErr, err)
		}
	}
	if document.Metadata != nil {
		if _, err := settings.bindSource(document.Metadata.Path()); err != nil {
			return all, outline, nil, errors.Join(analysisErr, err)
		}
	}
	for _, check := range composed.IncompleteChecks {
		s.checkCompletion.markUnavailableInvocation(check.Source, check.Invocation, check.Rule, check.Reason)
	}
	if len(registrations) != 0 {
		input, inputErr := composites.CheckInput(composed, path, sourcePath, content, document, references)
		input.Directory = root
		input.Revision, input.Coordinates = captured.Revision(), captured.Coordinates()
		analysisErr = errors.Join(analysisErr, inputErr)
		external, err := runDocumentAnalyzers(s.ctx, registrations, input, composed.Sources, usedRules)
		all = append(all, external...)
		analysisErr = errors.Join(analysisErr, err)
	}
	entryAliases := append(snapshot.Capture().Aliases(), path, snapshot.Reference().Document)
	for _, alias := range entryAliases {
		if relative, err := filepath.Rel(s.workingDir, alias); err == nil {
			entryAliases = append(entryAliases, relative)
		}
	}
	resultpolicy.InferReferences(all, sourcePath, references, entryAliases...)
	all, err = s.admitReportedSources(all, path, sourcePath, composed, rejected)
	analysisErr = errors.Join(analysisErr, err)
	canonicalPaths := map[string]string{root: canonicalRoot, s.workingDir: canonicalWorkingDir}
	for alias, index := range references {
		canonicalPaths[alias] = index.Document.Document
		canonicalPaths[index.Document.Document] = index.Document.Document
	}
	for _, alias := range entryAliases {
		canonicalPaths[alias] = captured.Revision().Document()
	}
	for _, finding := range all {
		if finding.Reference != nil && finding.Path != "" {
			canonicalPaths[finding.Path] = finding.Reference.Document
		}
	}
	for _, finding := range all {
		if finding.Binding.Valid() {
			continue
		}
		scope := entryBinding
		if prepared, exists := settings.bindings[finding.Path]; exists {
			scope = prepared
		}
		finding.Binding = scope.Rule(finding.Rule).ID()
	}
	policy := resultpolicy.Policy{Log: s.log, Debug: s.debug, Path: path, ConfigPath: sourcePath, Root: root, WorkingDir: s.workingDir, Content: content, ProjectRoot: projectRoot(project), Bindings: settings.registry.Seal(), Scope: entryBinding, SourceBindings: settings.bindings, Metadata: document.Metadata, AdditionalRules: names, IgnorePatterns: s.ignorePats}
	policy.EnforcedLifecycle = s.environment.EnforcedLifecycle
	policy.IgnoreSelectors = s.ignoreSelectors
	// Suppression findings use paths relative to the working directory. Add those
	// paths to the source-reference lookup to identify the declaring document.
	reported := maps.Clone(references)
	for document, index := range references {
		if relative, err := filepath.Rel(s.workingDir, document); err == nil {
			if _, exists := reported[relative]; !exists {
				reported[relative] = index
			}
		}
	}
	publication = &resultpolicy.Batch{Processor: resultpolicy.Processor{RuleSet: settings.rules, CanonicalPaths: canonicalPaths}, Policy: policy, Sources: composed.Sources, References: reported, EntryAliases: entryAliases}
	s.log("Collected", len(all), "findings in", time.Since(start).Milliseconds(), "ms for", path)
	return all, outline, publication, analysisErr
}

func (s *analysisSession) composeDocument(path, sourcePath, root string, content []byte, document parsing.Document, snapshot parsing.SourceSnapshot, action bool,
	project *workspace.Project, cfg, projectConfig *configuration.Config, settings *ruleConfigurations, provenance configuration.Provenance, processes *hostprocess.ProcessPool,
	actions *workspaceactions.Cache, workflows *workflowinputs.Cache, registrations []checkinputs.Registration, usedRules *[]rulemeta.Descriptor,
) (result composites.Result, resultErr error) {
	process := processes.Fork()
	var skippedMu sync.Mutex
	skipped := make(map[string]bool)
	// Adapter construction can fail before the composition service takes ownership.
	joined := false
	join := func() error { joined = true; return process.Finish() }
	defer func() {
		if !joined {
			resultErr = errors.Join(resultErr, process.Finish())
		}
		skippedMu.Lock()
		defer skippedMu.Unlock()
		invocations := indexCheckInvocations(result.Invocations)
		for name := range skipped {
			markUnavailable := func(ref sourcepos.Reference) {
				for _, context := range invocations.forSource(ref) {
					s.checkCompletion.markUnavailableInvocation(ref, context, name, "tool was unavailable for this analysis")
				}
			}
			markUnavailable(snapshot.Reference())
			for _, document := range result.SourceDocuments {
				revision := document.Capture().Revision()
				markUnavailable(sourcepos.Reference{Document: revision.Document(), Revision: revision.Digest()})
			}
		}
	}()
	var mappings map[string]string
	if cfg != nil {
		mappings = cfg.LocalActions
	}
	resolvedActions := workspaceactions.NewResolver(actions, mappings)
	host := s.repositories[projectRoot(project)]
	facts := configuration.RepositoryFacts(cfg, host)
	customLabels := runnerlang.LabelSet{}
	if known, available := facts.RunnerLabels.Value(); available {
		customLabels = runnerlang.KnownLabels(known.Values())
	}
	runnerOptions := s.environment.Runners
	if runnerOptions.Service == nil {
		runnerOptions.Service = &facts.Service
	}
	runners, runnerErr := runnerinput.PrepareWithLabels(s.ctx, snapshot, customLabels, runnerOptions, root)
	if report := runners.Report(); report.CacheKey != "" {
		report.Path = path
		s.runnerReportsMu.Lock()
		s.runnerReports = append(s.runnerReports, report)
		s.runnerReportsMu.Unlock()
	}
	defer func() { resultErr = errors.Join(resultErr, runnerErr) }()
	resolvedActions.SetRunners(runners)
	context := define.Inputs{Bindings: settings.bindings[sourcePath], SourceSnapshot: snapshot, Context: s.ctx, Path: path, Config: cfg, Actions: resolvedActions, WorkflowSources: workflows, Workflows: workflows.Resolved(), WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Reader: s.reader, WorkflowNames: s.workflowNames}
	context.Runners = runners
	context.Lifecycle = actionmeta.LifecycleContext{Service: *runnerOptions.Service, AsOf: runnerOptions.AsOf}
	context.Debug = s.debugWriter()
	context.Repository = host
	remoteActions := s.remoteActions[cfg.MetadataMirrorAccess()]
	context.RemoteActions = remoteActions.Resolved()
	if s.gitModes != nil {
		context.Index = s.gitModes.load
	}
	toolContext := tools.Context{AllowConfigProcesses: s.allowConfigProcesses, Config: cfg, ProjectConfig: configuration.InvocationPlanningConfig(cfg, projectConfig, projectRoot(project)), WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Processes: process, LoadEngine: s.loadScriptEngine, Debug: s.debug, Skipped: func(name string) {
		skippedMu.Lock()
		skipped[name] = true
		skippedMu.Unlock()
	}, Warn: func(err error) {
		if s.logOut != nil {
			_, _ = fmt.Fprintln(s.logOut, "warning:", err)
		}
	}}
	toolContext.Document = executor.SourceInput(document, path, content, snapshot.Reference())
	toolContext.Document.Revision, toolContext.Document.Coordinates = snapshot.Capture().Revision(), snapshot.Capture().Coordinates()
	selected, err := s.buildChecks(context, toolContext, action)
	defer func() {
		entryContext := report.CheckInvocation{}
		graph := result.Invocations
		if graph.Has(graph.RootID()) {
			entryContext = report.CheckInvocation{Entry: graph.Entry().Reference, ID: graph.RootID()}
		}
		for _, unavailable := range selected.Unavailable {
			s.checkCompletion.markUnavailableInvocation(snapshot.Reference(), entryContext, unavailable.Name, unavailable.Reason)
			s.debug("Skipped %s: %s", unavailable.Name, unavailable.Reason)
		}
	}()
	if err != nil {
		return result, err
	}
	for _, r := range selected.Rules {
		if participant, ok := r.(interface{ attachScope(*hostprocess.Scope) }); ok {
			participant.attachScope(process.Scope)
		}
	}
	requires := selection.RequiredFacts(registrations)
	options := composites.Options{
		Bindings: settings.bindings[sourcePath], BindSource: settings.bindInvocation,
		SourceSnapshots: s.snapshots,
		Captures:        s.captures,
		RemoteActions:   remoteActions,
		PrepareSource: func(_ parsing.SourceSnapshot, references []string) error {
			var lookups errgroup.Group
			lookups.SetLimit(concurrentMetadataLookups)
			for _, reference := range references {
				lookups.Go(func() error {
					_, err := remoteActions.Inspect(s.ctx, reference)
					return err
				})
			}
			return lookups.Wait()
		},
		Context: s.ctx, Actions: resolvedActions, Rules: selected.Rules, Scripts: selected.scripts,
		Runners: runners,
		ScriptInputsForScope: func(scope *binding.Scope) ([]checkinputs.ScriptInput, error) {
			return selected.inputSelectors(toolContext, scope)
		},
		Paths: execution.Paths{Workspace: root, Analysis: s.workingDir}, Reader: s.reader,
		ConfigProvenance: provenance,

		CollectScripts:   len(selected.scripts.Analyzers) != 0 || requires[rulemeta.Scripts] || requires[rulemeta.Invocations] || requires[rulemeta.Execution],
		ResolveExecution: selected.resolveExecution || requires[rulemeta.Execution], Join: join, Debug: s.debugWriter(),
	}
	if action {
		deployment, deploymentErr := scriptexecution.NewDeployment(s.snapshots, snapshot, sourcePath, document.Metadata.Path(), document.Metadata.SourceMapped)
		if deploymentErr != nil {
			return result, deploymentErr
		}
		result, err = composites.Action(options, snapshot, deployment, document.Metadata)
	} else {
		result, err = composites.Workflow(options, snapshot)
	}
	*usedRules = append(*usedRules, selected.ScriptInfo...)
	*usedRules = append(*usedRules, selection.Describe(ConfigurationRules(), selected.Rules)...)
	for _, r := range selected.Rules {
		s.debug("%s found %d errors", r.Name(), r.FindingCount())
	}
	return result, err
}
