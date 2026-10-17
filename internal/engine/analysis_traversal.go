package engine

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/rule"
)

// traversalHost owns rule construction and AST visiting. Scheduling and policy
// receive its result and cannot change the visitor while it is running.
type traversalHost struct {
	toolDescriptors []rule.Descriptor
	analysisLogger
	ctx            context.Context
	buildRules     func(ruleContext, bool) (builtinAnalyzers, error)
	workingDir     string
	reader         *fsread.Reader
	gitModes       *gitModes
	workflowNames  *workflownames.Index
	onRulesCreated func([]workflowvisit.Rule) []workflowvisit.Rule
	rulePresets    repoconfig.Presets
	requires       map[analyzer.Fact]bool
}

// traversalResult owns the collectors and work discovered by visiting one document.
// Traversal never starts script analyzers or waits for their tasks.
type traversalResult struct {
	rules            []workflowvisit.Rule
	builtins         builtinAnalyzers
	composites       *compositeAnalyzer
	metadataSources  map[string][]byte
	compositeConfigs map[string]*repoconfig.Config
}

func (l traversalHost) traverse(path, root string, w *workflowlang.Workflow, metadata *actionmeta.Metadata, project *workspace.Project, cfg, projectConfig *repoconfig.Config, proc *analyzerhost.ProcessPool, localActions *actionlocal.Cache, localReusableWorkflows *calls.Cache) (*traversalResult, error) {
	compositeConfigs := map[string]*repoconfig.Config{}
	dbg := l.debugWriter()
	var mappings map[string]string
	if cfg != nil {
		mappings = cfg.LocalActions
	}
	resolvedActions := actionlocal.NewResolver(localActions, mappings)

	c := ruleContext{
		path: path, config: cfg, projectConfig: projectConfig, actions: resolvedActions, workflows: localReusableWorkflows, process: proc,
		workflowNames: l.workflowNames, workingDir: l.workingDir, reader: l.reader, gitModes: l.gitModes,
	}
	if project != nil {
		c.projectRoot = project.RootDir()
	}
	build := l.buildRules
	if build == nil {
		build = registeredFactory(Request{}, l.analysisLogger)
	}
	builtins, err := build(c, metadata != nil)
	if err != nil {
		return nil, err
	}

	rules := builtins.rules
	if l.onRulesCreated != nil && metadata == nil {
		rules = l.onRulesCreated(rules)
	}

	v := NewVisitor()
	v.trackIgnores = hasConfiguredIgnores(cfg)
	for _, rule := range rules {
		if participant, ok := rule.(resourceParticipant); ok {
			participant.attachScope(proc.Scope)
		}
		v.AddPass(rule)
	}
	requires := l.requires
	v.composites = &compositeAnalyzer{
		ctx: l.ctx, actions: resolvedActions, passes: v.passes, scriptConfig: cfg, scriptPaths: execution.Paths{Workspace: root, Analysis: c.workingDir, Reader: c.reader},
		collectScripts:         len(builtins.scripts) != 0 || requires[analyzer.Scripts] || requires[analyzer.Invocations] || requires[analyzer.Execution],
		resolveScriptExecution: builtins.resolveExecution || requires[analyzer.Execution],
	}
	v.composites.configForFile = func(path string) (*repoconfig.Config, error) {
		config, err := repoconfig.ForAction(projectConfig, path, root)
		if err != nil {
			return nil, err
		}
		config = repoconfig.ResolveRules(l.rulePresets, config, l.toolDescriptors...)
		compositeConfigs[path] = config
		return config, nil
	}
	if dbg != nil {
		v.EnableDebug(dbg)
		for _, r := range rules {
			if participant, ok := r.(debugRule); ok {
				participant.EnableDebug(dbg)
			}
		}
	}
	if cfg != nil {
		for _, r := range rules {
			if participant, ok := r.(configuredRule); ok {
				participant.SetConfig(cfg)
			}
		}
	}

	var visitErr error
	if metadata != nil {
		visitErr = visitStandaloneAction(metadata, rules, v.composites)
	} else {
		visitErr = v.Visit(w)
	}
	if visitErr != nil {
		l.debug("Error occurred while visiting workflow syntax tree: %v", visitErr)
	}

	return &traversalResult{rules: rules, builtins: builtins, composites: v.composites, metadataSources: resolvedActions.Sources(), compositeConfigs: compositeConfigs}, visitErr
}

// collect runs after analyzer work and traversal resources have been joined.
func (t *traversalResult) collect(log analysisLogger, usedRules *[]RuleInfo) []*findingmodel.Finding {
	var all []*findingmodel.Finding
	for _, rule := range t.rules {
		errs := rule.Errs()
		log.debug("%s found %d errors", rule.Name(), len(errs))
		all = append(all, errs...)
	}
	for _, composite := range t.composites.rules {
		for _, rule := range composite.rules {
			for _, finding := range rule.Errs() {
				if origin := finding.SuppressionOrigin; origin != nil && origin.Path == "" {
					origin.Path = composite.meta.Path()
				}
				if finding.Path == "" {
					finding.Path = composite.meta.Path()
					finding.Source = composite.meta.Source
				}
				all = append(all, finding)
			}
		}
	}
	*usedRules = append(*usedRules, t.builtins.scriptInfo...)
	for _, rule := range t.rules {
		info, ok := repoconfig.FindRule(rule.Name())
		metadata := info.Descriptor
		if !ok {
			metadata.Name = rule.Name()
		}
		if described, ok := rule.(describedRule); ok {
			metadata.Description = described.Description()
		}
		*usedRules = append(*usedRules, metadata)
	}
	return all
}
