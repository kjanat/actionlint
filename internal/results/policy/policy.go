package policy

import (
	"fmt"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Policy supplies source text and the rule bindings prepared for this analysis.
type Policy struct {
	Path, ConfigPath, Root, WorkingDir string
	ProjectRoot                        string
	Content                            []byte

	EnforcedLifecycle []lifecycle.Policy
	Bindings          *binding.Snapshot
	Scope             *binding.Scope
	SourceBindings    map[string]*binding.Scope
	Metadata          *actionmeta.Metadata
	AdditionalRules   []string
	IgnorePatterns    configuration.IgnorePatterns
	IgnoreSelectors   IgnoreSelectors
	Log               func(...any)
	Debug             func(string, ...any)
	usedSuppression   func(string, sourcepos.Position, string)
}

func (processor Processor) evaluate(p Policy, all []*findingmodel.Finding, metadataSources map[string][]byte, authority map[*findingmodel.Finding]obligation) ([]*findingmodel.Finding, error) {
	path, configPath, root, content, cfg, metadata := p.Path, p.ConfigPath, p.Root, p.Content, p.Scope.Caller(), p.Metadata
	additionalRules := p.AdditionalRules
	var suppressionPolicy *configuration.SuppressionsPolicy
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
	}
	all, requiredLifecycle, err := processor.assessLifecycle(p, all, authority)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		all = processor.filterInlineSuppressionsAt(content, all, suppressionPolicy, "", p.inlineAudit(configPath, processor.levels(cfg)), additionalRules...)
		all = processor.filterConfigured(configPath, root, content, all, cfg, false, processor.levels(cfg), additionalRules...)
	}
	type occurrenceScope struct {
		path    string
		binding *binding.Scope
	}
	byPath := make(map[occurrenceScope][]*findingmodel.Finding)
	seenSources := make(map[string]bool)
	findingKey := func(findingPath string) string {
		if findingPath == "" {
			findingPath = path
		} else if p.ProjectRoot != "" && filepath.IsAbs(findingPath) {
			findingPath = processor.projectRelativePath(p.ProjectRoot, findingPath)
		}
		return findingPath
	}
	for _, finding := range all {
		if !finding.Binding.Valid() {
			finding.Binding = p.Scope.Rule(finding.Rule).ID()
		}
		key := findingKey(finding.Path)
		if origin := finding.SuppressionOrigin; origin != nil && origin.Path != "" {
			key = origin.Path
		} else if _, isMetadata := metadataSources[finding.Path]; isMetadata {
			key = finding.Path
		}
		prepared, exists := p.Bindings.Lookup(finding.Binding)
		if !exists {
			return nil, fmt.Errorf("finding %q has no prepared configuration binding", finding.Rule)
		}
		group := occurrenceScope{key, prepared.Scope()}
		byPath[group] = append(byPath[group], finding)
		seenSources[key] = true
	}
	foreignSources := map[string]string{}
	for sourcePath := range metadataSources {
		key := sourcePath
		foreignSources[key] = sourcePath
		if !seenSources[key] {
			scope, exists := p.SourceBindings[sourcePath]
			if !exists {
				return nil, fmt.Errorf("rule configuration was not prepared for source %q", sourcePath)
			}
			byPath[occurrenceScope{key, scope}] = nil
		}
	}
	all = nil
	for group, findings := range byPath {
		sourcePath, isMetadata := foreignSources[group.path]
		source := metadataSources[sourcePath]
		scope := group.binding
		diagnosticLevel := func(kind string) configuration.Level {
			return scope.DiagnosticLevel(kind)
		}
		if isMetadata && source != nil {
			prepared, exists := p.SourceBindings[sourcePath]
			if !exists {
				return nil, fmt.Errorf("rule configuration was not prepared for source %q", sourcePath)
			}
			findingConfig := prepared.Source()
			var policy *configuration.SuppressionsPolicy
			if findingConfig != nil {
				policy = findingConfig.Policy.DisallowSuppressions
			}
			findings = processor.filterForeignInline(sourcePath, source, findings, policy, p.inlineAudit(sourcePath, diagnosticLevel), additionalRules...)
			findings = processor.filterConfigured(sourcePath, root, source, findings, findingConfig, true, diagnosticLevel, additionalRules...)
		}
		for _, finding := range findings {
			if !finding.Binding.Valid() {
				finding.Binding = scope.Rule(finding.Rule).ID()
			}
			if _, exists := p.Bindings.Lookup(finding.Binding); !exists {
				return nil, fmt.Errorf("finding %q has a foreign configuration binding", finding.Rule)
			}
		}
		findings = slices.DeleteFunc(findings, func(e *findingmodel.Finding) bool {
			descriptor, _ := processor.RuleSet.FindRule(e.Rule)
			executionFailure := descriptor.ExecutionFailure && (e.Severity == "" || e.Severity == "error")
			e.NonBlocking = sourcepos.RemoteAddress(e.Path) && e.Path != p.Path &&
				!processor.RuleSet.DependencyFindingFails(cfg, e.Rule, e.Code, executionFailure)
			prepared, _ := p.Bindings.Lookup(e.Binding)
			level := prepared.Scope().DiagnosticLevel(e.Rule)
			if e.LifecycleEvidence != nil && e.Lifecycle != nil && level != "off" {
				return false
			}
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

	all = append(requiredLifecycle, all...)
	for _, err := range all {
		if err.Path == "" {
			err.Path = path // Populate filename in the error
		} else {
			err.Path = processor.displayPath(p.WorkingDir, err.Path)
		}
	}

	slices.SortFunc(all, findingmodel.Compare)

	return all, nil
}

func (p Policy) inlineAudit(path string, level func(string) configuration.Level) *inlineAudit {
	return &inlineAudit{level: level, used: func(position sourcepos.Position, rule string) {
		if p.usedSuppression != nil {
			p.usedSuppression(path, position, rule)
		}
	}}
}

func filterErrors(p Policy, errs []*findingmodel.Finding, cfgs []configuration.PathConfig) []*findingmodel.Finding {
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
func matchesIgnorePatterns(patterns configuration.IgnorePatterns, finding *findingmodel.Finding) bool {
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
