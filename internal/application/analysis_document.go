package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	documentmodel "actionlint.kjanat.dev/document"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/builtin"
	"actionlint.kjanat.dev/internal/checks"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/composition"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/rule"
	"actionlint.kjanat.dev/runner"
	"actionlint.kjanat.dev/source"
)

// analysisSession holds invocation resources assembled by the application.
// The workflow engine receives only prepared passes from the composition service.
type analysisSession struct {
	runnerReportsMu  sync.Mutex
	runnerReports    []runner.Report
	runners          runner.Options
	toolDescriptors  []rule.Descriptor
	tools            []tools.Registration
	analyzers        []analyzer.Registration
	loadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
	workflowNames    *workflownames.Index
	analysisLogger
	ctx            context.Context
	workingDir     string
	reader         *fsread.Reader
	gitModes       *gitModes
	ignorePats     repoconfig.IgnorePatterns
	onRulesCreated func([]workflowvisit.Rule) []workflowvisit.Rule
	rulePresets    repoconfig.Presets
}

// check dispatches the two document languages at the application input boundary.
func (s *analysisSession) check(path string, content []byte, project *workspace.Project, config *repoconfig.Config,
	processes *analyzerhost.ProcessPool, actions *actionlocal.Cache, workflows *calls.Cache, usedRules *[]rule.Descriptor,
) ([]*findingmodel.Finding, documentmodel.Outline, error) {
	sourcePath := path
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(s.workingDir, sourcePath)
	}
	if isActionInput(sourcePath) {
		document := documentparse.Action(path, sourcePath, content)
		return s.analyzeDocument(path, sourcePath, content, document, true, project, config, processes, actions, workflows, usedRules)
	}
	document := documentparse.Workflow(path, content)
	return s.analyzeDocument(path, sourcePath, content, document, false, project, config, processes, actions, workflows, usedRules)
}

func (s *analysisSession) analyzeDocument(path, sourcePath string, content []byte, document documentparse.Document, action bool,
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
	composed := composition.Result{Sources: map[string][]byte{}, Configs: map[string]*repoconfig.Config{}}
	if document.CanAnalyze() {
		composed, analysisErr = s.composeDocument(path, sourcePath, root, content, document, action, project, cfg, projectConfig, processes, actions, workflows, registrations, usedRules)
		if composed.Sources == nil {
			return nil, outline, analysisErr
		}
		all = append(all, composed.Findings...)
	}
	references := documentparse.References(sourcePath, content, document, composed.Sources, fsread.PhysicalPath)
	if len(registrations) != 0 {
		input := composed.AnalyzerInput(path, sourcePath, content, document, references)
		external, err := runDocumentAnalyzers(s.ctx, registrations, input, composed.Sources, usedRules)
		all = append(all, external...)
		analysisErr = errors.Join(analysisErr, err)
	}
	diagnosticpolicy.InferReferences(all, sourcePath, references)
	policy := diagnosticpolicy.Policy{RuleDescriptors: s.toolDescriptors, Log: s.log, Debug: s.debug, Path: path, ConfigPath: sourcePath, Root: root, WorkingDir: s.workingDir, Content: content, ProjectRoot: projectRoot(project), Config: cfg, ProjectConfig: projectConfig, Metadata: document.Metadata, Presets: s.rulePresets, AdditionalRules: names, IgnorePatterns: s.ignorePats}
	all, err = policy.Apply(all, composed.Sources, composed.Configs)
	if err != nil {
		return nil, outline, errors.Join(analysisErr, err)
	}
	s.log("Found total", len(all), "errors in", time.Since(start).Milliseconds(), "ms for", path)
	return all, outline, analysisErr
}

func (s *analysisSession) composeDocument(path, sourcePath, root string, content []byte, document documentparse.Document, action bool,
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
	var mappings map[string]string
	if cfg != nil {
		mappings = cfg.LocalActions
	}
	resolvedActions := actionlocal.NewResolver(actions, mappings)
	var customLabels []string
	if cfg != nil {
		customLabels = cfg.SelfHostedRunner.Labels
	}
	runners, runnerErr := runnerlang.Prepare(s.ctx, document.Workflow, customLabels, s.runners, root)
	if runners.Report.Identity != "" {
		report := runners.Report
		report.Path = path
		s.runnerReportsMu.Lock()
		s.runnerReports = append(s.runnerReports, report)
		s.runnerReportsMu.Unlock()
	}
	defer func() { resultErr = errors.Join(resultErr, runnerErr) }()
	resolvedActions.Runners = runners
	context := builtin.Context{Context: s.ctx, Path: path, Config: cfg, Actions: resolvedActions, Workflows: workflows, WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Reader: s.reader, WorkflowNames: s.workflowNames}
	context.Runners = runners
	if s.gitModes != nil {
		context.Index = s.gitModes.load
	}
	toolContext := tools.Context{Config: cfg, ProjectConfig: projectConfig, WorkingDir: s.workingDir, ProjectRoot: projectRoot(project), Processes: process, LoadEngine: s.loadScriptEngine, Debug: s.debug, Warn: func(err error) {
		if s.logOut != nil {
			_, _ = fmt.Fprintln(s.logOut, "warning:", err)
		}
	}}
	toolContext.DocumentPath, toolContext.DocumentContent = path, content
	var mainReferences workflowfacts.References
	var indexMain sync.Once
	toolContext.Reference = func(filename string, snapshot []byte, position diagnostic.Position) source.Reference {
		var refs workflowfacts.References
		if filename != "" && filename != path && filename != sourcePath {
			parsed := documentparse.Action(filename, filename, snapshot)
			refs = documentparse.References(filename, snapshot, parsed, nil, fsread.PhysicalPath)[filename]
		} else {
			indexMain.Do(func() {
				mainReferences = documentparse.References(sourcePath, content, document, nil, fsread.PhysicalPath)[sourcePath]
			})
			refs = mainReferences
		}
		if ref, ok := refs.Positions[[2]int{position.Line, position.Column}]; ok {
			return ref
		}
		return refs.Document
	}
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
	service := composition.New(composition.Options{Context: s.ctx, Actions: resolvedActions, Rules: selected.Rules, Scripts: selected.scripts,
		Runners: runners,
		Paths:   execution.Paths{Workspace: root, Analysis: s.workingDir, Reader: s.reader}, Config: cfg,
		ConfigForFile: func(path string) (*repoconfig.Config, error) {
			config, err := repoconfig.ForAction(projectConfig, path, root)
			if err != nil {
				return nil, err
			}
			return repoconfig.ResolveRules(s.rulePresets, config, s.toolDescriptors...), nil
		},
		CollectScripts:   len(selected.scripts.Analyzers) != 0 || requires[analyzer.Scripts] || requires[analyzer.Invocations] || requires[analyzer.Execution],
		ResolveExecution: selected.resolveExecution || requires[analyzer.Execution], Join: join, Debug: s.debugWriter()})
	if action {
		result, err = service.Action(document.Metadata)
	} else {
		result, err = service.Workflow(document.Workflow)
	}
	*usedRules = append(*usedRules, selected.ScriptInfo...)
	*usedRules = append(*usedRules, checks.Describe(selected.Rules)...)
	for _, r := range selected.Rules {
		s.debug("%s found %d errors", r.Name(), len(r.Errs()))
	}
	return result, err
}
