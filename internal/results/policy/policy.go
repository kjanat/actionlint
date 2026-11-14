package policy

import (
	"fmt"
	"path/filepath"
	"slices"

	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Policy supplies source text, prepared rule settings and suppression reporting hooks.
type Policy struct {
	Path, ConfigPath, Root, WorkingDir string
	ProjectRoot                        string
	Content                            []byte
	Config                             *repoconfig.Config
	// SourceConfigurations uses the source paths passed to Processor.Apply.
	// A present nil entry selects defaults; a missing entry is an error.
	SourceConfigurations map[string]*repoconfig.Config
	Metadata             *actionmeta.Metadata
	AdditionalRules      []string
	IgnorePatterns       repoconfig.IgnorePatterns
	IgnoreSelectors      IgnoreSelectors
	Log                  func(...any)
	Debug                func(string, ...any)
	// UsedSuppression records a matched selector for invocation-wide reconciliation.
	UsedSuppression func(string, sourcepos.Position, string)
}

// Apply filters completed findings using each source's resolved policy and normalizes their output order.
// It updates the caller-owned findings and metadataSources in place; findings
// without a path take p's path and, for action metadata, share p.Content.
func (processor Processor) Apply(p Policy, all []*findingmodel.Finding, metadataSources map[string][]byte, compositeConfigs map[string]*repoconfig.Config) ([]*findingmodel.Finding, error) {
	path, configPath, root, content, cfg, metadata := p.Path, p.ConfigPath, p.Root, p.Content, p.Config, p.Metadata
	additionalRules := p.AdditionalRules
	var suppressionPolicy *repoconfig.SuppressionsPolicy
	if cfg != nil {
		suppressionPolicy = cfg.Policy.DisallowSuppressions
	}
	if metadata != nil {
		metadataSources[metadata.Path()] = content
		for _, finding := range all {
			if finding.Path == "" {
				finding.Path, finding.Source = metadata.Path(), content
			}
		}
	} else {
		all = processor.filterInlineSuppressionsAt(content, all, suppressionPolicy, "", p.inlineAudit(configPath, processor.Levels(cfg)), additionalRules...)
		all = processor.FilterConfigured(configPath, root, content, all, cfg, false, processor.Levels(cfg), additionalRules...)
	}
	byPath := make(map[string][]*findingmodel.Finding)
	findingKey := func(findingPath string) string {
		if findingPath == "" {
			findingPath = path
		} else if p.ProjectRoot != "" && filepath.IsAbs(findingPath) {
			findingPath = processor.projectRelativePath(p.ProjectRoot, findingPath)
		}
		return findingPath
	}
	for _, finding := range all {
		key := findingKey(finding.Path)
		if origin := finding.SuppressionOrigin; origin != nil && origin.Path != "" {
			key = origin.Path
		} else if _, isMetadata := metadataSources[finding.Path]; isMetadata {
			key = finding.Path
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
		diagnosticLevel := func(kind string) repoconfig.Level {
			levelConfig := cfg
			if cfg == nil || cfg.Lint.Enabled == nil || *cfg.Lint.Enabled {
				descriptor, _ := processor.RuleSet.FindRule(kind)
				switch descriptor.Scope {
				case rulescope.SourceConfiguration:
					levelConfig = findingConfig
				case rulescope.CompositeConfiguration:
					if compositeConfig, ok := compositeConfigs[sourcePath]; ok {
						levelConfig = compositeConfig
					}
				case rulescope.CallerConfiguration:
					// Caller-scoped rules use the caller configuration.
				}
			}
			return processor.RuleSet.DiagnosticLevel(levelConfig, kind)
		}
		if isMetadata && source != nil {
			var prepared bool
			findingConfig, prepared = p.SourceConfigurations[sourcePath]
			if !prepared {
				return nil, fmt.Errorf("rule configuration was not prepared for source %q", sourcePath)
			}
			var policy *repoconfig.SuppressionsPolicy
			if findingConfig != nil {
				policy = findingConfig.Policy.DisallowSuppressions
			}
			findings = processor.filterForeignInline(sourcePath, source, findings, policy, p.inlineAudit(sourcePath, diagnosticLevel), additionalRules...)
			findings = processor.FilterConfigured(sourcePath, root, source, findings, findingConfig, true, diagnosticLevel, additionalRules...)
		}
		findings = slices.DeleteFunc(findings, func(e *findingmodel.Finding) bool {
			descriptor, _ := processor.RuleSet.FindRule(e.Rule)
			executionFailure := descriptor.ExecutionFailure && (e.Severity == "" || e.Severity == "error")
			e.NonBlocking = sourcepos.RemoteAddress(e.Path) && e.Path != p.Path &&
				!processor.RuleSet.DependencyFindingFails(cfg, e.Rule, e.Code, executionFailure)
			level := diagnosticLevel(e.Rule)
			switch level {
			case "off":
				return true
			case "warn":
				e.Severity = "warning"
			case "info":
				e.Severity = "info"
			case "error":
				e.Severity = "error"
			}
			return false
		})
		for _, finding := range findings {
			if p.IgnoreSelectors.matches(processor.RuleSet, finding.Diagnostic) {
				continue
			}
			all = append(all, filterErrors(p, []*findingmodel.Finding{finding}, cfg.PathConfigs(findingKey(finding.Path)))...)
		}
	}

	all = findingmodel.FirstVisibleMetadata(all)
	for _, err := range all {
		if err.Path == "" {
			err.Path = path // Populate filename in the error
		} else {
			err.Path = processor.displayPath(p.WorkingDir, err.Path)
		}
	}

	slices.SortFunc(all, findingmodel.Compare)
	all = slices.CompactFunc(all, findingmodel.Equal) // Alias may duplicate errors

	return all, nil
}

func (p Policy) inlineAudit(path string, level func(string) repoconfig.Level) *inlineAudit {
	return &inlineAudit{level: level, used: func(position sourcepos.Position, rule string) {
		if p.UsedSuppression != nil {
			p.UsedSuppression(path, position, rule)
		}
	}}
}

func filterErrors(p Policy, errs []*findingmodel.Finding, cfgs []repoconfig.PathConfig) []*findingmodel.Finding {
	if len(p.IgnorePatterns) == 0 && len(cfgs) == 0 {
		return errs
	}

	filtered := make([]*findingmodel.Finding, 0, len(errs))
Loop:
	for _, err := range errs {
		if matchesIgnorePatterns(p.IgnorePatterns, err) {
			debug(p, "Error %q is ignored due to --ignore-regex command line option", err.Message)
			continue Loop
		}
		for _, c := range cfgs {
			if matchesIgnorePatterns(c.Ignore, err) {
				debug(p, "Error %q is ignored due to the \"ignore\" config in the config file", err.Message)
				continue Loop
			}
		}
		filtered = append(filtered, err)
	}
	if len(filtered) != len(errs) {
		log(p, "Filtered", len(errs)-len(filtered), "error(s) due to \"--ignore-regex\" command line option and \"ignore\" configuration")
	}
	return filtered
}

// Code-based filters keep working when messages no longer repeat the code.
// Match the plain message too, preserving existing message-anchored patterns.
func matchesIgnorePatterns(patterns repoconfig.IgnorePatterns, finding *findingmodel.Finding) bool {
	return len(patterns) != 0 && (patterns.Match(finding.Message) ||
		finding.Code != "" && patterns.Match(finding.Code+": "+finding.Message))
}

func debug(p Policy, format string, args ...any) {
	if p.Debug != nil {
		p.Debug(format, args...)
	}
}

func log(p Policy, args ...any) {
	if p.Log != nil {
		p.Log(args...)
	}
}
