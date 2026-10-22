package engine

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/internal/buildinfo"
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/filefilter"
	"actionlint.kjanat.dev/internal/repoconfig"
	"actionlint.kjanat.dev/internal/suppression"
)

func presetLevel(p RulePreset, d ruleDescriptor, fallback RuleLevel) RuleLevel {
	switch p {
	case "none":
		return "off"
	case "all":
		if !d.isNursery() {
			return "on"
		}
		return fallback
	case "recommended":
		if d.Recommended {
			return "on"
		}
		return "off"
	default:
		return fallback
	}
}

func ruleSetting(cfg *Config, d ruleDescriptor, p RulePresets) RuleSetting {
	setting := RuleSetting{Level: "on"}
	if !d.Recommended {
		setting.Level = "off"
	}
	if d.configure != nil && d.enabled != nil {
		setting.Level = "off"
		if d.enabled(ruleContext{config: cfg}) {
			setting.Level = "on"
		}
	}
	var rules LintRulesConfig
	if cfg != nil {
		rules = cfg.Lint.Rules
	}
	setting.Level = presetLevel(rules.Preset, d, setting.Level)
	if p.Strict && !d.isNursery() {
		setting.Level = "on"
	}
	if p.Experimental != nil && *p.Experimental && d.isNursery() {
		setting.Level = "on"
	}
	group := rules.Groups()[d.Category]
	if d.isNursery() && group.Preset == "all" {
		setting.Level = "on"
	} else {
		setting.Level = presetLevel(group.Preset, d, setting.Level)
	}
	if group.Level != "" && group.Level != "default" {
		setting.Level = group.Level
	}
	if explicit, ok := group.Rules[d.Name]; ok {
		if explicit.Level == "" || explicit.Level == "default" {
			explicit.Level = setting.Level
		}
		setting = explicit
	}
	if d.isNursery() && p.Experimental != nil && !*p.Experimental {
		setting.Level = "off"
	}
	if slices.Contains(rules.Disable, d.Name) || cfg != nil && cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
		setting.Level = "off"
	}
	return setting
}

func diagnosticLevel(cfg *Config, name string) RuleLevel {
	if cfg != nil {
		if cfg.Lint.Enabled != nil && !*cfg.Lint.Enabled {
			return "off"
		}
		if level, ok := cfg.Lint.Rules.ResolvedLevel(name); ok {
			return level
		}
	}
	if d, ok := findRuleDescriptor(name); ok {
		return ruleSetting(cfg, d, RulePresets{}).Level
	}
	return "on"
}

func applyRulePresets(p RulePresets, config *Config) *Config {
	if config == nil && !p.Strict && p.Experimental == nil {
		return nil
	}
	next := Config{}
	if config != nil {
		next = *config
	}
	resolved := make(map[string]RuleLevel)
	next.Lint.Rules.SetResolved(resolved)
	for _, d := range builtinRuleDescriptors() {
		setting := ruleSetting(config, d, p)
		resolved[d.Name] = setting.Level
		if d.configure != nil {
			d.configure(&next, setting)
		}
	}
	return &next
}

func configForDocument(config *Config, path, root string) (*Config, error) {
	if isActionInput(path) {
		return repoconfig.ForAction(config, path, root)
	}
	return repoconfig.ForFile(config, path, root)
}

func filterConfigIgnores(path, root string, source []byte, findings []*Error, cfg *Config, foreign bool, level func(string) RuleLevel) []*Error {
	if cfg == nil || len(cfg.Ignores) == 0 || len(findings) == 0 {
		return findings
	}
	contexts := suppression.Contexts(source, exprlang.LiteralExpressionValue)
	filtered := make([]*Error, 0, len(findings))
	for _, finding := range findings {
		reportedPath := path
		if finding.Filepath != "" {
			reportedPath = finding.Filepath
		}
		file := filepath.ToSlash(repoconfig.RepositoryRelativePath(reportedPath, root))
		if level(finding.Kind) == "off" {
			filtered = append(filtered, finding)
			continue
		}
		line, column, applies := finding.Line, finding.Column, finding.source == nil || foreign
		if origin := finding.suppressionOrigin; origin != nil {
			line, applies = origin.first, origin.path == path || !foreign && origin.path == ""
			column = 0
		}
		ignored := false
		if applies && finding.Kind != "disallow-suppressions" {
			for _, ignore := range cfg.Ignores {
				if finding.Kind != ignore.Rule || ignore.Validate() != nil {
					continue
				}
				if ignore.File != "" && !(filefilter.Selection{Includes: []string{ignore.File}}).Match(file) {
					continue
				}
				matches := contexts.Match(line, column, ignore.Job, ignore.Step, ignore.Uses)
				if finding.ignoreOrigin != nil {
					matches = finding.ignoreOrigin.matches(ignore.Job, ignore.Step, ignore.Uses)
					if finding.ignoreOrigin.metadata {
						matches = (ignore.Job == "" || ignore.Job == finding.ignoreOrigin.job) && contexts.Match(line, column, "", ignore.Step, ignore.Uses)
					}
				}
				if !matches {
					continue
				}
				mode := cfg.Policy.DisallowSuppressions.ReportFor(ignore.Rule)
				if mode == repoconfig.ReportSuppression || mode == repoconfig.ReportAll {
					policyError := *finding
					policyError.Kind = "disallow-suppressions"
					policyError.Message = fmt.Sprintf("configured suppression of %s is disallowed by policy.disallow-suppressions", ignore.Rule)
					policyError.code, policyError.severity, policyError.fixes = "", "", nil
					filtered = append(filtered, &policyError)
				}
				ignored = mode == repoconfig.SuppressionsAllowed || mode == repoconfig.ReportSuppression
				break
			}
		}
		if !ignored {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

type analysisConfigState struct {
	sync.Mutex
	source   *repoconfig.Loaded
	overlays []ConfigOverlay
	onLoaded func(ConfigReport)
	loaded   map[*Project]*Config
	reports  map[*Project]ConfigReport
}

func (a *AnalysisSession) configForProject(project *Project) (*Config, error) {
	var cfg *Config
	var source *repoconfig.Loaded
	if a.defaultConfig != nil {
		cfg = a.defaultConfig
		if a.configState != nil {
			source = a.configState.source
		}
	} else if project != nil {
		cfg, source = project.Config(), project.config
	}
	s := a.configState
	if s == nil {
		return cfg, nil
	}
	s.Lock()
	var notification *ConfigReport
	defer func() {
		s.Unlock()
		if notification != nil && s.onLoaded != nil {
			s.onLoaded(*notification)
		}
	}()
	if loaded, ok := s.loaded[project]; ok {
		return loaded, nil
	}
	report := ConfigReport{Explicit: a.defaultConfig != nil}
	if project != nil {
		report.Project = project.RootDir()
	}
	if source != nil {
		report.File = source.Filename
	}
	for _, overlay := range s.overlays {
		report.Overrides = append(report.Overrides, overlay.Name())
	}
	cfg, resolved, err := repoconfig.Overlaid(cfg, source, s.overlays)
	if err != nil {
		return nil, err
	}
	report.Inspection = ConfigInspection{Path: report.File, Config: resolved.Values, Origins: resolved.Origins, Warnings: resolved.Warnings}
	if s.onLoaded == nil {
		for _, warning := range resolved.Warnings {
			file := warning.File
			if file == "" {
				file = report.File
			}
			_, _ = fmt.Fprintf(a.logOut, "%s:%d:%d: warning: %s\n", file, warning.Line, warning.Column, warning.Message)
		}
	}
	s.loaded[project] = cfg
	s.reports[project] = report
	notification = &report
	return cfg, nil
}

func (run *shellcheckRun) pathContext() repoconfig.PathContext {
	configDir := run.paths.workspace
	if config := run.cfg; config.Path() != "" {
		configDir = filepath.Dir(config.Path())
	}
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = run.paths.workspace
	}
	actionPath := run.actionPath
	if runnerPath := os.Getenv("GITHUB_ACTION_PATH"); actionPath != "" && runnerPath != "" {
		local, localErr := os.Stat(actionPath)
		runner, runnerErr := os.Stat(runnerPath)
		if localErr == nil && runnerErr == nil && os.SameFile(local, runner) {
			actionPath = runnerPath
		}
	}
	return repoconfig.PathContext{ConfigDir: configDir, GitDir: run.paths.workspace, Workspace: workspace, ActionPath: actionPath}
}

//go:embed config_default.yaml
var defaultConfigTemplate string

func writeDefaultConfigFile(path string) error {
	info, _ := debug.ReadBuildInfo()
	ref := buildinfo.SourceRef(Version(), info)
	b := []byte(strings.Replace(defaultConfigTemplate, "/HEAD/actionlint.schema.json", "/"+ref+"/actionlint.schema.json", 1))
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}

func diagnosticLevels(cfg *Config) func(string) RuleLevel {
	return func(name string) RuleLevel { return diagnosticLevel(cfg, name) }
}
