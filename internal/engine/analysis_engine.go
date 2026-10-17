package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"actionlint.kjanat.dev/analyzer"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/report"
	"actionlint.kjanat.dev/internal/workflow/calls"
	workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/rule"
)

type analysisEngine struct {
	toolDescriptors []rule.Descriptor
	analyzers       []analyzer.Registration
	workflowNames   *workflownames.Index
	analysisLogger
	ctx            context.Context
	buildRules     func(ruleContext, bool) (builtinAnalyzers, error)
	workingDir     string
	reader         *fsread.Reader
	gitModes       *gitModes
	ignorePats     repoconfig.IgnorePatterns
	onRulesCreated func([]workflowvisit.Rule) []workflowvisit.Rule
	rulePresets    repoconfig.Presets
}

func (l *analysisEngine) check(
	path string,
	content []byte,
	project *workspace.Project,
	cfg *repoconfig.Config,
	proc *analyzerhost.ProcessPool,
	localActions *actionlocal.Cache,
	localReusableWorkflows *calls.Cache,
	usedRules *[]RuleInfo,
) (findings []*findingmodel.Finding, outline report.DocumentOutline, resultErr error) {
	proc = proc.Fork()
	joined := false
	defer func() {
		if !joined {
			resultErr = errors.Join(resultErr, proc.Finish())
		}
	}()
	// Each call owns its rules; caches and process scheduling are shared across files.
	root := l.workingDir
	if project != nil {
		root = project.RootDir()
	}
	var configErr error
	projectConfig := cfg
	configPath := path
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(l.workingDir, configPath)
	}
	cfg, configErr = repoconfig.ForDocument(cfg, configPath, root)
	if configErr != nil {
		if isActionInput(configPath) {
			outline, _ := ParseActionOutline(path, content)
			return nil, outline, configErr
		}
		w, all := Parse(content)
		return nil, workflowOutline(path, w, len(all) != 0), configErr
	}
	additionalRules := registeredRuleNames(l.analyzers)
	for _, descriptor := range l.toolDescriptors {
		additionalRules = append(additionalRules, descriptor.Name)
	}
	if err := repoconfig.ValidateRuleNames(cfg, additionalRules...); err != nil {
		return nil, nil, err
	}
	cfg = repoconfig.ResolveRules(l.rulePresets, cfg, l.toolDescriptors...)
	registrations, err := configuredAnalyzers(cfg, l.analyzers)
	if err != nil {
		return nil, nil, err
	}

	var start time.Time
	if l.logLevel >= LogLevelVerbose {
		start = time.Now()
	}

	l.log("Linting", path)
	if project != nil {
		l.log("Using project at", project.RootDir())
	}

	if cfg != nil {
		l.debug("Config: %#v", cfg)
	} else {
		l.debug("No config was found")
	}

	parsed := parseDocument(path, configPath, content)
	w, metadata, all := parsed.workflow, parsed.metadata, parsed.findings
	outline = parsed.outline
	var analysisErr error
	metadataSources := map[string][]byte{}
	compositeConfigs := map[string]*repoconfig.Config{}

	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found", len(all), "parse errors in", elapsed.Milliseconds(), "ms for", path)
	}

	var units []scriptUnit
	var facts factAvailability

	if w != nil {
		host := traversalHost{analysisLogger: l.analysisLogger, ctx: l.ctx, buildRules: l.buildRules, workingDir: l.workingDir, reader: l.reader, gitModes: l.gitModes, workflowNames: l.workflowNames, onRulesCreated: l.onRulesCreated, rulePresets: l.rulePresets, requires: requiredFacts(registrations), toolDescriptors: l.toolDescriptors}
		traversal, err := host.traverse(path, root, w, metadata, project, cfg, projectConfig, proc, localActions, localReusableWorkflows)
		if traversal == nil {
			return nil, outline, err
		}
		analysisErr = err
		units, facts = traversal.composites.scripts, factAvailability{scripts: traversal.composites.collectScripts, execution: traversal.composites.resolveScriptExecution}
		scriptFindings, scriptErr := runScriptAnalyzers(l.ctx, traversal.builtins.scripts, scriptBatch{scopes: traversal.composites.scriptScopes, scripts: units})
		all = append(all, scriptFindings...)
		analysisErr = errors.Join(analysisErr, scriptErr, proc.Finish())
		joined = true
		all = append(all, traversal.collect(l.analysisLogger, usedRules)...)
		metadataSources = traversal.metadataSources
		compositeConfigs = traversal.compositeConfigs
	}

	if len(registrations) != 0 {
		input := parsed.analyzerInput(path, configPath, content, units, facts, metadataSources)
		externalFindings, externalErr := runDocumentAnalyzers(l.ctx, registrations, input, metadataSources, usedRules)
		all = append(all, externalFindings...)
		analysisErr = errors.Join(analysisErr, externalErr)
	}

	references := map[string]workflowfacts.References{}
	rootWorkflow := w
	if metadata != nil && w != nil {
		rootWorkflow = actionFactsWorkflow(metadata)
	}
	references[configPath] = workflowfacts.Index(configPath, content, rootWorkflow, metadata != nil)
	for filename, source := range metadataSources {
		parsed := parseDocument(filename, filename, source)
		if parsed.metadata != nil && parsed.workflow != nil {
			references[filename] = workflowfacts.Index(filename, source, actionFactsWorkflow(parsed.metadata), true)
		}
	}
	for _, finding := range all {
		if finding.Reference != nil {
			continue
		}
		filename := configPath
		if finding.Source != nil && finding.Path != "" {
			filename = finding.Path
		}
		refs, ok := references[filename]
		if !ok {
			continue
		}
		ref := refs.Document
		if at, exists := refs.Positions[[2]int{finding.Start.Line, finding.Start.Column}]; exists {
			ref = at
		}
		owner := finding.Owner
		if owner == nil && finding.IgnoreOrigin != nil {
			owner = &findingmodel.Owner{Job: finding.IgnoreOrigin.Job, Step: finding.IgnoreOrigin.StepPosition}
		}
		if owner != nil && !finding.RawScriptSource {
			if job, exists := refs.Jobs[strings.ToLower(owner.Job)]; exists {
				ref = job
			}
			if owner.Step != nil {
				if step, exists := refs.Positions[[2]int{owner.Step.Line, owner.Step.Col}]; exists {
					ref = step
				}
			}
		}

		finding.Reference = &ref
		finding.InferredReference = true
	}
	filter := diagnosticpolicy.Policy{RuleDescriptors: l.toolDescriptors, Log: l.log, Debug: l.debug, Path: path, ConfigPath: configPath, Root: root, WorkingDir: l.workingDir, Content: content, ProjectRoot: projectRoot(project), Config: cfg, ProjectConfig: projectConfig, Metadata: metadata, Presets: l.rulePresets, AdditionalRules: additionalRules, IgnorePatterns: l.ignorePats}
	all, policyErr := filter.Apply(all, metadataSources, compositeConfigs)
	if policyErr != nil {
		return nil, outline, errors.Join(analysisErr, policyErr)
	}
	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found total", len(all), "errors in", elapsed.Milliseconds(), "ms for", path)
	}

	return all, outline, analysisErr
}
