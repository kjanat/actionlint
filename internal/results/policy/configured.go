package policy

import (
	"fmt"
	"path/filepath"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/selection"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

// FilterConfigured applies resolved ignore selectors to the same logical invocation.
func (processor Processor) FilterConfigured(path, root string, source []byte, findings []*findingmodel.Finding, cfg *repoconfig.Config, foreign bool, level func(string) repoconfig.Level, additional ...string) []*findingmodel.Finding {
	if cfg == nil || len(cfg.Ignores) == 0 || len(findings) == 0 {
		return findings
	}
	contexts := suppression.Contexts(source, exprsyntax.LiteralExpressionValue)
	filtered := make([]*findingmodel.Finding, 0, len(findings))
	for _, finding := range findings {
		reportedPath := path
		if finding.Path != "" {
			reportedPath = finding.Path
		}
		file := filepath.ToSlash(processor.projectRelativePath(root, reportedPath))
		if level(finding.Rule) == "off" {
			filtered = append(filtered, finding)
			continue
		}
		line, column, applies := finding.Start.Line, finding.Start.Column, finding.Source == nil || foreign
		if origin := finding.SuppressionOrigin; origin != nil {
			line, applies = origin.First, origin.Path == path || !foreign && origin.Path == ""
			column = 0
		}
		ignored := false
		if applies && finding.Rule != "forbidden-suppression" {
			for _, ignore := range cfg.Ignores {
				if processor.RuleSet.ResolveAlias(finding.Rule) != processor.RuleSet.ResolveAlias(ignore.Rule) || processor.RuleSet.ValidateIgnore(ignore, additional...) != nil {
					continue
				}
				if ignore.File != "" && !selection.MatchPatterns([]string{ignore.File}, nil, file) {
					continue
				}
				matches := contexts.Match(line, column, ignore.Job, ignore.Step, ignore.Uses)
				if finding.IgnoreOrigin != nil {
					matches = finding.IgnoreOrigin.Matches(ignore.Job, ignore.Step, ignore.Uses)
					if finding.IgnoreOrigin.Metadata {
						matches = (ignore.Job == "" || ignore.Job == finding.IgnoreOrigin.Job) && contexts.Match(line, column, "", ignore.Step, ignore.Uses)
					}
				}
				if !matches {
					continue
				}
				mode := cfg.Policy.DisallowSuppressions.ReportFor(processor.RuleSet.ResolveAlias(ignore.Rule))
				if mode == repoconfig.ReportSuppression || mode == repoconfig.ReportAll {
					policyError := *finding
					policyError.Rule = "forbidden-suppression"
					policyError.Message = fmt.Sprintf("configured suppression of %s is disallowed by policy.forbidden-suppression", ignore.Rule)
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

// Levels projects resolved rule severity lookup without exposing traversal.
func (processor Processor) Levels(cfg *repoconfig.Config) func(string) repoconfig.Level {
	return func(name string) repoconfig.Level { return processor.RuleSet.DiagnosticLevel(cfg, name) }
}
