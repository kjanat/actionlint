package engine

import (
	"fmt"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/internal/config"
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/filefilter"
	"actionlint.kjanat.dev/internal/suppression"
)

func diagnosticLevel(cfg *Config, name string) RuleLevel {
	return repoconfig.DiagnosticLevel(cfg, name)
}
func configForDocument(cfg *Config, path, root string) (*Config, error) {
	return repoconfig.ForDocument(cfg, path, root)
}

func filterConfigIgnores(path, root string, source []byte, findings []*Error, cfg *Config, foreign bool, level func(string) RuleLevel, additional ...string) []*Error {
	if cfg == nil || len(cfg.Ignores) == 0 || len(findings) == 0 {
		return findings
	}
	contexts := suppression.Contexts(source, exprlang.LiteralExpressionValue)
	filtered := make([]*Error, 0, len(findings))
	for _, finding := range findings {
		reportedPath := path
		if finding.Path != "" {
			reportedPath = finding.Path
		}
		file := filepath.ToSlash(repoconfig.RepositoryRelativePath(reportedPath, root))
		if level(finding.Rule) == "off" {
			filtered = append(filtered, finding)
			continue
		}
		line, column, applies := finding.Start.Line, finding.Start.Column, finding.source == nil || foreign
		if origin := finding.suppressionOrigin; origin != nil {
			line, applies = origin.first, origin.path == path || !foreign && origin.path == ""
			column = 0
		}
		ignored := false
		if applies && finding.Rule != "disallow-suppressions" {
			for _, ignore := range cfg.Ignores {
				if finding.Rule != ignore.Rule || ignore.Validate(additional...) != nil {
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
					policyError.Rule = "disallow-suppressions"
					policyError.Message = fmt.Sprintf("configured suppression of %s is disallowed by policy.disallow-suppressions", ignore.Rule)
					policyError.Code, policyError.Severity, policyError.Fixes = "", "", nil
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

func writeDefaultConfigFile(path string) error { return repoconfig.WriteDefaultFile(path, Version()) }

func diagnosticLevels(cfg *Config) func(string) RuleLevel {
	return func(name string) RuleLevel { return diagnosticLevel(cfg, name) }
}
