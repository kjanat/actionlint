package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	documentmodel "actionlint.kjanat.dev/v2/document"

	"actionlint.kjanat.dev/v2/analyzer"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/checks"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/tools"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	workflownames "actionlint.kjanat.dev/v2/internal/workflow/names"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/runner"
)

// analysisSession holds invocation resources assembled by the application.
// The workflow engine receives only prepared passes from the composition service.
type analysisSession struct {
	runnerReportsMu      sync.Mutex
	runnerReports        []runner.Report
	runners              runner.Options
	toolDescriptors      []rule.Descriptor
	tools                []tools.Registration
	analyzers            []analyzer.Registration
	loadScriptEngine     func(context.Context, string) (analyzer.ScriptBackend, error)
	allowConfigProcesses bool
	workflowNames        *workflownames.Index
	analysisLogger
	ctx            context.Context
	workingDir     string
	reader         *fsread.Reader
	captures       *capture.Store
	snapshots      *documentparse.SourceSnapshots
	gitModes       *gitModes
	ignorePats     repoconfig.IgnorePatterns
	onRulesCreated func([]workflowvisit.Rule) []workflowvisit.Rule
	rulePresets    repoconfig.Presets
}

// check dispatches the two document languages at the application input boundary.
func (s *analysisSession) check(path, sourcePath string, captured capture.Snapshot, project *workspace.Project, config *repoconfig.Config,
	processes *analyzerhost.ProcessPool, actions *actionlocal.Cache, workflows *calls.Cache, usedRules *[]rule.Descriptor,
) ([]*findingmodel.Finding, documentmodel.Outline, error) {
	if s.captures == nil || s.snapshots == nil {
		return nil, nil, errors.New("analysis requires result-owned source captures")
	}
	content := captured.Bytes()
	if err := s.captures.Verify(captured, content); err != nil {
		return nil, nil, err
	}
	if isActionInput(sourcePath) {
		document := documentparse.Action(path, sourcePath, content)
		return s.analyzeDocument(path, sourcePath, captured, content, document, true, project, config, processes, actions, workflows, usedRules)
	}
	document := documentparse.Workflow(path, content)
	return s.analyzeDocument(path, sourcePath, captured, content, document, false, project, config, processes, actions, workflows, usedRules)
}

func (s *analysisSession) analyzeDocument(path, sourcePath string, captured capture.Snapshot, content []byte, document documentparse.Document, action bool,
	project *workspace.Project, projectConfig *repoconfig.Config, processes *analyzerhost.ProcessPool,
	actions *actionlocal.Cache, workflows *calls.Cache, usedRules *[]rule.Descriptor,
) (findings []*findingmodel.Finding, outline documentmodel.Outline, resultErr error) {
	outline = document.Outline
	root := s.workingDir
	if project != nil {
		root = project.RootDir()
	}
	cfg, err := repoconfig.ForDocument(projectConfig, sourcePath, root)
	if err != nil {
		return nil, outline, err
	}
	names := checks.RuleNames(s.analyzers)
	for _, d := range s.toolDescriptors {
		names = append(names, d.Name)
	}
	if err := repoconfig.ValidateRuleNames(cfg, names...); err != nil {
		return nil, nil, err
	}
	cfg = repoconfig.ResolveRules(s.rulePresets, cfg, s.toolDescriptors...)
	registrations, err := checks.ConfigureAnalyzers(cfg, s.analyzers)
	if err != nil {
		return nil, nil, err
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
	s.log("Found", len(document.Findings), "parse errors in", time.Since(start).Milliseconds(), "ms for", path)

	all := document.Findings
	var analysisErr error
	snapshot, err := s.snapshots.Document(captured, document)
	if err != nil {
		return all, outline, err
	}
	// The request's display spelling is an authorized entry address. Resolve it
	// through this capture owner before sealing, so metadata aliases (including
	// Windows drive spelling) need no post-publication filesystem fallback.
	if path != "" {
		displayAddress := path
		if path != "<stdin>" && !filepath.IsAbs(path) {
			displayAddress = filepath.Join(s.workingDir, path)
		}
		if _, err := s.captures.Canonical(s.ctx, displayAddress); err != nil {
			return all, outline, err
		}
	}
	composed := composition.Result{EntrySnapshot: snapshot, Sources: map[string][]byte{}, Configs: map[string]*repoconfig.Config{}}
	if document.CanAnalyze() {
		composed, analysisErr = s.composeDocument(path, sourcePath, root, content, document, snapshot, action, project, cfg, projectConfig, processes, actions, workflows, registrations, usedRules)
		if composed.Sources == nil {
			return nil, outline, analysisErr
		}
		all = append(all, composed.Findings...)
	}
	references := map[string]workflowfacts.References{sourcePath: snapshot.CanonicalReferences()}
	rejected, sourceErr := s.admitSourceDocuments(composed, references)
	analysisErr = errors.Join(analysisErr, sourceErr)
	if len(registrations) != 0 {
		input, inputErr := composed.AnalyzerInput(path, sourcePath, content, document, references)
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
	diagnosticpolicy.InferReferences(all, sourcePath, references, entryAliases...)
	all, err = s.admitReportedSources(all, path, sourcePath, composed, rejected)
	analysisErr = errors.Join(analysisErr, err)
	policy := diagnosticpolicy.Policy{RuleDescriptors: s.toolDescriptors, Log: s.log, Debug: s.debug, Path: path, ConfigPath: sourcePath, Root: root, WorkingDir: s.workingDir, Content: content, ProjectRoot: projectRoot(project), Config: cfg, ProjectConfig: projectConfig, Metadata: document.Metadata, Presets: s.rulePresets, AdditionalRules: names, IgnorePatterns: s.ignorePats}
	all, err = policy.Apply(all, composed.Sources, composed.Configs)
	if err != nil {
		return nil, outline, errors.Join(analysisErr, err)
	}
	s.log("Found total", len(all), "errors in", time.Since(start).Milliseconds(), "ms for", path)
	return all, outline, analysisErr
}

func (s *analysisSession) composeDocument(path, sourcePath, root string, content []byte, document documentparse.Document, snapshot *documentparse.SourceSnapshot, action bool,
	project *workspace.Project, cfg, projectConfig *repoconfig.Config, processes *analyzerhost.ProcessPool,
	actions *actionlocal.Cache, workflows *calls.Cache, registrations []analyzer.Registration, usedRules *[]rule.Descriptor,
) (result composition.Result, resultErr error) {
	process := processes.Fork()
	// Adapter construction can fail before the composition service takes ownership.
	joined := false
	join := func() error { joined = true; return process.Finish() }
	defer func() {
		if !joined {
			resultErr = errors.Join(resultErr, process.Finish())
		}
	}()
	snapshots := snapshot.Registry()
	var mappings map[string]string
	if cfg != nil {
		mappings = cfg.LocalActions
	}
	resolvedActions := actionlocal.NewResolver(actions, mappings)
	var customLabels []string
	if cfg != nil {
		customLabels = cfg.SelfHostedRunner.Labels
	}
	runners, runnerErr := runnerlang.Prepare(s.ctx, snapshot, customLabels, s.runners, root)
	if runners.Report.Identity != "" {
		report := runners.Report
		report.Path = path
		s.runnerReportsMu.Lock()
		s.runnerReports = append(s.runnerReports, report)
		s.runnerReportsMu.Unlock()
	}
	defer func() { resultErr = errors.Join(resultErr, runnerErr) }()
	resolvedActions.Runners = runners
	context := builtin.Context{SourceSnapshot: snapshot, Context: s.ctx, Path: path, Config: cfg, Actions: resolvedActions, Workflows: workflows, WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Reader: s.reader, WorkflowNames: s.workflowNames}
	context.Runners = runners
	if s.gitModes != nil {
		context.Index = s.gitModes.load
	}
	toolContext := tools.Context{AllowConfigProcesses: s.allowConfigProcesses, Config: cfg, ProjectConfig: projectConfig, WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Processes: process, LoadEngine: s.loadScriptEngine, Debug: s.debug, Warn: func(err error) {
		if s.logOut != nil {
			_, _ = fmt.Fprintln(s.logOut, "warning:", err)
		}
	}}
	toolContext.Document = document.AnalyzerSource(path, content, snapshot.Reference())
	toolContext.Document.Revision, toolContext.Document.Coordinates = snapshot.Capture().Revision(), snapshot.Capture().Coordinates()
	selected, err := s.buildChecks(context, toolContext, action)
	if err != nil {
		return result, err
	}
	for _, r := range selected.Rules {
		if participant, ok := r.(interface{ attachScope(*analyzerhost.Scope) }); ok {
			participant.attachScope(process.Scope)
		}
	}
	requires := checks.RequiredFacts(registrations)
	service := composition.New(composition.Options{
		SourceSnapshots: snapshots,
		Context:         s.ctx, Actions: resolvedActions, Rules: selected.Rules, Scripts: selected.scripts,
		Runners: runners,
		ScriptInputsForConfig: func(cfg *repoconfig.Config) ([]analyzer.ScriptInput, error) {
			return selected.inputSelectors(toolContext, cfg)
		},
		Paths: execution.Paths{Workspace: root, Analysis: s.workingDir, Reader: s.reader}, Config: cfg,
		ConfigForFile: func(path string) (*repoconfig.Config, error) {
			config, err := repoconfig.ForAction(projectConfig, path, root)
			if err != nil {
				return nil, err
			}
			return repoconfig.ResolveRules(s.rulePresets, config, s.toolDescriptors...), nil
		},
		CollectScripts:   len(selected.scripts.Analyzers) != 0 || requires[analyzer.Scripts] || requires[analyzer.Invocations] || requires[analyzer.Execution],
		ResolveExecution: selected.resolveExecution || requires[analyzer.Execution], Join: join, Debug: s.debugWriter(),
	})
	if action {
		deployment, deploymentErr := scriptexecution.NewDeployment(snapshot, sourcePath, document.Metadata.Path(), document.Metadata.SourceMapped)
		if deploymentErr != nil {
			return result, deploymentErr
		}
		result, err = service.Action(snapshot, deployment, document.Metadata)
	} else {
		result, err = service.Workflow(snapshot)
	}
	*usedRules = append(*usedRules, selected.ScriptInfo...)
	*usedRules = append(*usedRules, checks.Describe(selected.Rules)...)
	for _, r := range selected.Rules {
		s.debug("%s found %d errors", r.Name(), len(r.Errs()))
	}
	return result, err
}
