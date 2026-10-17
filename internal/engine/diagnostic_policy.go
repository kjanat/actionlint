package engine

import (
	"path/filepath"
	"slices"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/rule"
)

// diagnosticPolicy applies source-specific suppressions and severity after all analyzers finish.
// It has no access to traversal, process scheduling or backend lifetimes.
type diagnosticPolicy struct {
	analysisLogger
	path, configPath, root, workingDir string
	content                            []byte
	project                            *Project
	config, projectConfig              *Config
	metadata                           *ActionMetadata
	presets                            RulePresets
	additionalRules                    []string
	ruleDescriptors                    []rule.Descriptor
	ignorePats                         IgnorePatterns
}

func (p diagnosticPolicy) apply(all []*Error, metadataSources map[string][]byte, compositeConfigs map[string]*Config) ([]*Error, error) {
	path, configPath, root, content, project, cfg, projectConfig, metadata := p.path, p.configPath, p.root, p.content, p.project, p.config, p.projectConfig, p.metadata
	additionalRules := p.additionalRules
	var suppressionPolicy *SuppressionsPolicy
	if cfg != nil {
		suppressionPolicy = cfg.Policy.DisallowSuppressions
	}
	if metadata != nil {
		metadataSources[metadata.Path()] = content
		for _, finding := range all {
			if finding.Path == "" {
				finding.Path, finding.source = metadata.Path(), content
			}
		}
	} else {
		all = filterInlineSuppressions(content, all, suppressionPolicy, additionalRules...)
		all = filterConfigIgnores(configPath, root, content, all, cfg, false, diagnosticLevels(cfg), additionalRules...)
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
			// Tools run in the resolved working directory. Compare both
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
		key := findingKey(finding.Path)
		if origin := finding.suppressionOrigin; origin != nil && origin.path != "" {
			key = origin.path
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
			return diagnosticLevel(levelConfig, kind)
		}
		if isMetadata && source != nil {
			var err error
			findingConfig, err = repoconfig.ForSuppressions(projectConfig, sourcePath, root)
			if err != nil {
				return nil, err
			}
			findingConfig = repoconfig.ResolveRules(p.presets, findingConfig, p.ruleDescriptors...)
			var policy *SuppressionsPolicy
			if findingConfig != nil {
				policy = findingConfig.Policy.DisallowSuppressions
			}
			findings = filterForeignInlineSuppressions(sourcePath, source, findings, policy, additionalRules...)
			findings = filterConfigIgnores(sourcePath, root, source, findings, findingConfig, true, diagnosticLevel, additionalRules...)
		}
		findings = slices.DeleteFunc(findings, func(e *Error) bool {
			switch diagnosticLevel(e.Rule) {
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
			all = append(all, p.filterErrors([]*Error{finding}, cfg.PathConfigs(findingKey(finding.Path)))...)
		}
	}

	all = firstVisibleMetadataFindings(all)
	for _, err := range all {
		if err.Path == "" {
			err.Path = path // Populate filename in the error
		} else {
			err.Path = relativeAnalysisPath(p.workingDir, err.Path)
		}
	}

	slices.SortFunc(all, compareErrors)
	all = slices.CompactFunc(all, equalsErrors) // Alias may duplicate errors

	return all, nil
}
func (p diagnosticPolicy) filterErrors(errs []*Error, cfgs []PathConfig) []*Error {
	if len(p.ignorePats) == 0 && len(cfgs) == 0 {
		return errs
	}

	filtered := make([]*Error, 0, len(errs))
Loop:
	for _, err := range errs {
		if p.ignorePats.Match(err.Message) {
			p.debug("Error %q is ignored due to -ignore command line option", err.Message)
			continue Loop
		}
		for _, c := range cfgs {
			if c.Ignore.Match(err.Message) {
				p.debug("Error %q is ignored due to the \"ignore\" config in the config file", err.Message)
				continue Loop
			}
		}
		filtered = append(filtered, err)
	}
	if len(filtered) != len(errs) {
		p.log("Filtered", len(errs)-len(filtered), "error(s) due to \"-ignore\" command line option and \"ignore\" configuration")
	}
	return filtered
}
