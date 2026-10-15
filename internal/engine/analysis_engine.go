package engine

import (
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflownames"
	"go.yaml.in/yaml/v4"
)

type analysisEngine struct {
	analyzers     []analyzer.Registration
	workflowNames *workflownames.Index
	analysisLogger
	ctx            context.Context
	buildRules     func(ruleContext, bool) ([]Rule, error)
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
	usedRules *[]Rule,
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
	cfg, configErr = configForFile(cfg, configPath, root)
	if configErr != nil {
		if isActionInput(configPath) {
			outline, _ := ParseActionOutline(path, content)
			return nil, outline, configErr
		}
		w, all := Parse(content)
		return nil, workflowOutline(path, w, len(all) != 0), configErr
	}
	cfg = l.rulePresets.apply(cfg)

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

	var w *Workflow
	var all []*Error
	var metadata *ActionMetadata
	if isActionInput(configPath) {
		metadata = &ActionMetadata{}
		err := yaml.Unmarshal(content, metadata)
		metadata.dir, metadata.file, metadata.src = filepath.Dir(configPath), filepath.Base(configPath), content
		outline = actionOutline(path, metadata, err)
		if err != nil {
			all = handleYAMLUnmarshalError(err)
			for _, finding := range all {
				finding.Line, finding.Column = max(1, finding.Line), max(1, finding.Column)
			}
		} else {
			w = &Workflow{}
		}
	} else {
		w, all = Parse(content)
		outline = workflowOutline(path, w, len(all) != 0)
	}
	var analysisErr error
	metadataSources := map[string][]byte{}
	compositeConfigs := map[string]*Config{}

	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found", len(all), "parse errors in", elapsed.Milliseconds(), "ms for", path)
	}

	if w != nil {
		dbg := l.debugWriter()
		localActions = &LocalActionsCache{base: localActions, usedSources: make(map[string][]byte), validation: newActionMetadataValidation()}
		if cfg != nil {
			localActions.sourceMappings = cfg.LocalActions
		}

		c := ruleContext{path: path, config: cfg, projectConfig: projectConfig, actions: localActions, workflows: localReusableWorkflows, process: proc,
			workflowNames: l.workflowNames, workingDir: l.workingDir, reader: l.reader, gitModes: l.gitModes}
		if project != nil {
			c.projectRoot = project.RootDir()
		}
		build := l.buildRules
		if build == nil {
			build = builtinFactory(AnalysisRequest{}, l.analysisLogger)
		}
		rules, err := build(c, metadata != nil)
		if err != nil {
			return nil, outline, err
		}

		if l.onRulesCreated != nil && metadata == nil {
			rules = l.onRulesCreated(rules)
		}

		v := NewVisitor()
		for _, rule := range rules {
			if participant, ok := rule.(resourceParticipant); ok {
				participant.attachScope(proc.scope)
			}
			v.AddPass(rule)
		}
		v.composites = &compositeAnalyzer{ctx: l.ctx, actions: localActions, passes: v.passes}
		v.composites.configForFile = func(path string) (*Config, error) {
			config, err := configForFile(projectConfig, path, root)
			if err != nil {
				return nil, err
			}
			config = l.rulePresets.apply(config)
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

		if metadata != nil {
			analysisErr = visitStandaloneAction(metadata, rules, v.composites)
		} else {
			analysisErr = v.Visit(w)
		}
		if analysisErr != nil {
			l.debug("Error occurred while visiting workflow syntax tree: %v", analysisErr)
		}
		analysisErr = errors.Join(analysisErr, proc.finish())
		joined = true

		for _, rule := range rules {
			errs := rule.Errs()
			l.debug("%s found %d errors", rule.Name(), len(errs))
			all = append(all, errs...)
		}
		for _, composite := range v.composites.rules {
			for _, rule := range composite.rules {
				for _, finding := range rule.Errs() {
					if origin := finding.suppressionOrigin; origin != nil && origin.path == "" {
						origin.path = composite.meta.Path()
					}
					if finding.Filepath == "" {
						finding.Filepath = composite.meta.Path()
						finding.source = composite.meta.src
					}
					all = append(all, finding)
				}
			}
		}
		maps.Copy(metadataSources, localActions.usedSources)

		*usedRules = rules
	}

	if len(l.analyzers) != 0 {
		factsWorkflow := w
		if metadata != nil && w != nil {
			factsWorkflow = actionFactsWorkflow(metadata)
		}
		input := documentAnalyzerInput(path, content, factsWorkflow, outline)
		findings, err := analyzerhost.Run(l.ctx, input, l.analyzers)
		analysisErr = errors.Join(analysisErr, err)
		for _, finding := range findings {
			all = append(all, legacyDiagnosticError(finding))
		}
	}

	var suppressionPolicy *SuppressionsPolicy
	if cfg != nil {
		suppressionPolicy = cfg.Policy.DisallowSuppressions
	}
	if metadata != nil {
		metadataSources[metadata.Path()] = content
		for _, finding := range all {
			if finding.Filepath == "" {
				finding.Filepath, finding.source = metadata.Path(), content
			}
		}
	} else {
		all = filterInlineSuppressions(content, all, suppressionPolicy)
		all = filterConfigIgnores(configPath, root, content, all, cfg, false, cfg.diagnosticLevel)
	}
	var projectDir string
	if project != nil {
		projectDir = project.RootDir()
		if resolved, err := filepath.EvalSymlinks(projectDir); err == nil {
			projectDir = resolved
		}
	}
	byPath := make(map[string][]*Error)
	findingKey := func(findingPath string) string {
		if findingPath == "" {
			findingPath = path
		} else if project != nil && filepath.IsAbs(findingPath) {
			// ShellCheck runs in the resolved working directory. Compare both
			// paths physically so checkout aliases retain project-relative filters.
			if resolved, err := filepath.EvalSymlinks(findingPath); err == nil {
				findingPath = resolved
			}
			if relative, err := filepath.Rel(projectDir, findingPath); err == nil {
				findingPath = relative
			}
		}
		return findingPath
	}
	for _, finding := range all {
		key := findingKey(finding.Filepath)
		if origin := finding.suppressionOrigin; origin != nil && origin.path != "" {
			key = origin.path
		} else if _, isMetadata := metadataSources[finding.Filepath]; isMetadata {
			key = finding.Filepath
		}
		byPath[key] = append(byPath[key], finding)
	}
	foreignSources := map[string]string{}
	for sourcePath := range metadataSources {
		key := sourcePath
		foreignSources[key] = sourcePath
		if _, exists := byPath[key]; !exists {
			byPath[key] = nil
		}
	}
	all = nil
	for scopePath, findings := range byPath {
		sourcePath, isMetadata := foreignSources[scopePath]
		source := metadataSources[sourcePath]
		findingConfig := cfg
		diagnosticLevel := func(kind string) RuleLevel {
			levelConfig := cfg
			if cfg == nil || cfg.Lint.Enabled == nil || *cfg.Lint.Enabled {
				descriptor, _ := findRuleDescriptor(kind)
				switch descriptor.configurationScope {
				case sourceConfiguration:
					levelConfig = findingConfig
				case compositeConfiguration:
					if compositeConfig, ok := compositeConfigs[sourcePath]; ok {
						levelConfig = compositeConfig
					}
				}
			}
			return levelConfig.diagnosticLevel(kind)
		}
		if isMetadata && source != nil {
			var err error
			findingConfig, err = suppressionConfigForFile(projectConfig, sourcePath, root)
			if err != nil {
				return nil, outline, err
			}
			findingConfig = l.rulePresets.apply(findingConfig)
			var policy *SuppressionsPolicy
			if findingConfig != nil {
				policy = findingConfig.Policy.DisallowSuppressions
			}
			findings = filterForeignInlineSuppressions(sourcePath, source, findings, policy)
			findings = filterConfigIgnores(sourcePath, root, source, findings, findingConfig, true, diagnosticLevel)
		}
		findings = slices.DeleteFunc(findings, func(e *Error) bool {
			switch diagnosticLevel(e.Kind) {
			case "off":
				return true
			case "warn":
				e.severity = "warning"
			case "info":
				e.severity = "info"
			case "error":
				e.severity = "error"
			}
			return false
		})
		for _, finding := range findings {
			all = append(all, l.filterErrors([]*Error{finding}, cfg.PathConfigs(findingKey(finding.Filepath)))...)
		}
	}

	all = firstVisibleMetadataFindings(all)
	for _, err := range all {
		if err.Filepath == "" {
			err.Filepath = path // Populate filename in the error
		} else {
			err.Filepath = relativeAnalysisPath(l.workingDir, err.Filepath)
		}
	}

	slices.SortFunc(all, compareErrors)
	all = slices.CompactFunc(all, equalsErrors) // Alias may duplicate errors

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

func (l *analysisEngine) filterErrors(errs []*Error, cfgs []PathConfig) []*Error {
	if len(l.ignorePats) == 0 && len(cfgs) == 0 {
		return errs
	}

	filtered := make([]*Error, 0, len(errs))
Loop:
	for _, err := range errs {
		if l.ignorePats.Match(err) {
			l.debug("Error %q is ignored due to -ignore command line option", err.Message)
			continue Loop
		}
		for _, c := range cfgs {
			if c.Ignore.Match(err) {
				l.debug("Error %q is ignored due to the \"ignore\" config in the config file", err.Message)
				continue Loop
			}
		}
		filtered = append(filtered, err)
	}
	if len(filtered) != len(errs) {
		l.log("Filtered", len(errs)-len(filtered), "error(s) due to \"-ignore\" command line option and \"ignore\" configuration")
	}
	return filtered
}
