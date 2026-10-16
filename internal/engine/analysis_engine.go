package engine

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"actionlint.kjanat.dev/internal/repoconfig"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflownames"
)

type analysisEngine struct {
	analyzers     []analyzer.Registration
	workflowNames *workflownames.Index
	analysisLogger
	ctx            context.Context
	buildRules     func(ruleContext, bool) (builtinAnalyzers, error)
	workingDir     string
	reader         *fsread.Reader
	gitModes       *gitModes
	ignorePats     IgnorePatterns
	onRulesCreated func([]Rule) []Rule
	rulePresets    RulePresets
}

func (l *analysisEngine) check(
	path string,
	content []byte,
	project *Project,
	cfg *Config,
	proc *concurrentProcess,
	localActions *LocalActionsCache,
	localReusableWorkflows *LocalReusableWorkflowCache,
	usedRules *[]RuleInfo,
) (findings []*Error, outline DocumentOutline, resultErr error) {
	proc = proc.fork()
	joined := false
	defer func() {
		if !joined {
			resultErr = errors.Join(resultErr, proc.finish())
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
	cfg, configErr = configForDocument(cfg, configPath, root)
	if configErr != nil {
		if isActionInput(configPath) {
			outline, _ := ParseActionOutline(path, content)
			return nil, outline, configErr
		}
		w, all := Parse(content)
		return nil, workflowOutline(path, w, len(all) != 0), configErr
	}
	additionalRules := registeredRuleNames(l.analyzers)
	if err := repoconfig.ValidateRuleNames(cfg, additionalRules...); err != nil {
		return nil, nil, err
	}
	cfg = applyRulePresets(l.rulePresets, cfg)

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
	compositeConfigs := map[string]*Config{}

	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found", len(all), "parse errors in", elapsed.Milliseconds(), "ms for", path)
	}

	var units []scriptUnit
	var facts factAvailability

	if w != nil {
		host := traversalHost{analysisLogger: l.analysisLogger, ctx: l.ctx, buildRules: l.buildRules, workingDir: l.workingDir, reader: l.reader, gitModes: l.gitModes, workflowNames: l.workflowNames, onRulesCreated: l.onRulesCreated, rulePresets: l.rulePresets, requires: requiredFacts(l.analyzers)}
		traversal, err := host.traverse(path, root, w, metadata, project, cfg, projectConfig, proc, localActions, localReusableWorkflows)
		if traversal == nil {
			return nil, outline, err
		}
		analysisErr = err
		units, facts = traversal.composites.scripts, factAvailability{scripts: traversal.composites.collectScripts, execution: traversal.composites.resolveScriptExecution}
		scriptFindings, scriptErr := runScriptAnalyzers(l.ctx, traversal.builtins.scripts, scriptBatch{scopes: traversal.composites.scriptScopes, scripts: units})
		all = append(all, scriptFindings...)
		analysisErr = errors.Join(analysisErr, scriptErr, proc.finish())
		joined = true
		all = append(all, traversal.collect(l.analysisLogger, usedRules)...)
		metadataSources = traversal.metadataSources
		compositeConfigs = traversal.compositeConfigs
	}

	if len(l.analyzers) != 0 {
		input := parsed.analyzerInput(path, configPath, content, units, facts)
		externalFindings, externalErr := runDocumentAnalyzers(l.ctx, l.analyzers, input, metadataSources, usedRules)
		all = append(all, externalFindings...)
		analysisErr = errors.Join(analysisErr, externalErr)
	}

	policy := diagnosticPolicy{analysisLogger: l.analysisLogger, path: path, configPath: configPath, root: root, workingDir: l.workingDir, content: content, project: project, config: cfg, projectConfig: projectConfig, metadata: metadata, presets: l.rulePresets, additionalRules: additionalRules, ignorePats: l.ignorePats}
	all, policyErr := policy.apply(all, metadataSources, compositeConfigs)
	if policyErr != nil {
		return nil, outline, errors.Join(analysisErr, policyErr)
	}
	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found total", len(all), "errors in", elapsed.Milliseconds(), "ms for", path)
	}

	return all, outline, analysisErr
}

func relativeAnalysisPath(directory, source string) string {
	if !filepath.IsAbs(source) {
		return source
	}
	// In-memory sources need not exist on disk. Use physical paths only when
	// both resolve; otherwise retain both original spellings.
	if resolvedDirectory, err := filepath.EvalSymlinks(directory); err == nil {
		if resolvedSource, err := filepath.EvalSymlinks(source); err == nil {
			directory, source = resolvedDirectory, resolvedSource
		}
	}
	if relative, err := filepath.Rel(directory, source); err == nil {
		return relative
	}
	return source
}
