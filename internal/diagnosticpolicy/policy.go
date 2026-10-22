package diagnosticpolicy

import (
	"path/filepath"
	"slices"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/rule"
)

// Policy applies source-specific suppressions and severity after all analyzers finish.
// It has no access to traversal, process scheduling or backend lifetimes.
type Policy struct {
	Path, ConfigPath, Root, WorkingDir string
	ProjectRoot                        string
	Content                            []byte
	Config, ProjectConfig              *repoconfig.Config
	Metadata                           *actionmeta.Metadata
	Presets                            repoconfig.Presets
	AdditionalRules                    []string
	RuleDescriptors                    []rule.Descriptor
	IgnorePatterns                     repoconfig.IgnorePatterns
	Log                                func(...any)
	Debug                              func(string, ...any)
}

// Apply filters completed findings using each source's resolved policy and normalizes their output order.
func (p Policy) Apply(all []*findingmodel.Finding, metadataSources map[string][]byte, compositeConfigs map[string]*repoconfig.Config) ([]*findingmodel.Finding, error) {
	path, configPath, root, content, cfg, projectConfig, metadata := p.Path, p.ConfigPath, p.Root, p.Content, p.Config, p.ProjectConfig, p.Metadata
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
		all = FilterInline(content, all, suppressionPolicy, additionalRules...)
		all = FilterConfigured(configPath, root, content, all, cfg, false, Levels(cfg), additionalRules...)
	}
	var projectDir string
	if p.ProjectRoot != "" {
		projectDir = p.ProjectRoot
		if resolved, err := filepath.EvalSymlinks(projectDir); err == nil {
			projectDir = resolved
		}
	}
	byPath := make(map[string][]*findingmodel.Finding)
	findingKey := func(findingPath string) string {
		if findingPath == "" {
			findingPath = path
		} else if p.ProjectRoot != "" && filepath.IsAbs(findingPath) {
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
				descriptor, _ := repoconfig.FindRule(kind)
				switch descriptor.Scope {
				case repoconfig.SourceScope:
					levelConfig = findingConfig
				case repoconfig.CompositeScope:
					if compositeConfig, ok := compositeConfigs[sourcePath]; ok {
						levelConfig = compositeConfig
					}
				}
			}
			return repoconfig.DiagnosticLevel(levelConfig, kind)
		}
		if isMetadata && source != nil {
			var err error
			findingConfig, err = repoconfig.ForSuppressions(projectConfig, sourcePath, root)
			if err != nil {
				return nil, err
			}
			findingConfig = repoconfig.ResolveRules(p.Presets, findingConfig, p.RuleDescriptors...)
			var policy *repoconfig.SuppressionsPolicy
			if findingConfig != nil {
				policy = findingConfig.Policy.DisallowSuppressions
			}
			findings = FilterForeignInline(sourcePath, source, findings, policy, additionalRules...)
			findings = FilterConfigured(sourcePath, root, source, findings, findingConfig, true, diagnosticLevel, additionalRules...)
		}
		findings = slices.DeleteFunc(findings, func(e *findingmodel.Finding) bool {
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
			all = append(all, p.filterErrors([]*findingmodel.Finding{finding}, cfg.PathConfigs(findingKey(finding.Path)))...)
		}
	}

	all = findingmodel.FirstVisibleMetadata(all)
	for _, err := range all {
		if err.Path == "" {
			err.Path = path // Populate filename in the error
		} else {
			err.Path = RelativePath(p.WorkingDir, err.Path)
		}
	}

	slices.SortFunc(all, findingmodel.Compare)
	all = slices.CompactFunc(all, findingmodel.Equal) // Alias may duplicate errors

	return all, nil
}

func (p Policy) filterErrors(errs []*findingmodel.Finding, cfgs []repoconfig.PathConfig) []*findingmodel.Finding {
	if len(p.IgnorePatterns) == 0 && len(cfgs) == 0 {
		return errs
	}

	filtered := make([]*findingmodel.Finding, 0, len(errs))
Loop:
	for _, err := range errs {
		if p.IgnorePatterns.Match(err.Message) {
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

func (p Policy) debug(format string, args ...any) {
	if p.Debug != nil {
		p.Debug(format, args...)
	}
}

func (p Policy) log(args ...any) {
	if p.Log != nil {
		p.Log(args...)
	}
}
