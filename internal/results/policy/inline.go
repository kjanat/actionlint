package policy

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/ruleconfig"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

// SuppressionRules returns the ordered rule catalogue for inline directives.
func (processor Processor) SuppressionRules(additional ...string) []string {
	rules := processor.RuleSet.Rules()
	names := make([]string, 0, len(rules)+len(additional))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return append(names, additional...)
}

type inlineAudit struct {
	level func(string) ruleconfig.Level
	used  func(sourcetext.Position, string)
	// declared receives each rule a directive names, used or not.
	declared func(sourcetext.Position, string)
}

func (processor Processor) filterInlineSuppressionsAt(source []byte, findings []*finding.Finding, policy *configuration.SuppressionsPolicy, scope string, audit *inlineAudit, additional ...string) []*finding.Finding {
	allowedRules := processor.SuppressionRules(additional...)
	directives, invalid := suppression.ParseInlineSuppressions(source, allowedRules, processor.RuleSet.Names())
	if audit != nil && audit.declared != nil {
		for _, directive := range directives {
			for _, name := range directive.Rules {
				audit.declared(directive.Position, name)
			}
		}
	}
	evaluation, reports := suppression.Evaluate(directives, func(name string) suppression.Decision {
		mode := policy.ReportFor(processor.RuleSet.ResolveAlias(name))
		return suppression.Decision{
			Suppress: mode == configuration.SuppressionsAllowed || mode == configuration.ReportSuppression,
			Report:   mode == configuration.ReportSuppression || mode == configuration.ReportAll,
		}
	}, processor.RuleSet.Names())
	for _, d := range invalid {
		findings = append(findings, finding.FromDiagnostic(d))
	}
	filtered := make([]*finding.Finding, 0, len(findings)+len(reports))
	type selector struct {
		directive int
		rule      string
	}
	used := make(map[selector]bool)
	for _, finding := range findings {
		lines, applies := sourcetext.LineRange{First: finding.Start.Line, Last: finding.Start.Line}, finding.Source.Content == nil
		targetPath := scope
		if origin := finding.SuppressionOrigin; origin != nil {
			lines, applies = origin.Lines, origin.Path == scope
			targetPath = origin.Path
		}
		if audit != nil && applies && audit.level(finding.Rule) != "off" && finding.Rule != "unused-suppression" {
			for index, directive := range directives {
				for _, selected := range directive.Rules {
					if !lines.Overlaps(directive.Lines) || processor.RuleSet.ResolveAlias(selected) != processor.RuleSet.ResolveAlias(finding.Rule) {
						continue
					}
					used[selector{index, selected}] = true
					if audit.used != nil {
						audit.used(directive.Position, selected)
					}
					break
				}
			}
		}
		if applies && finding.Rule != "inline-suppression" && processor.RuleSet.ResolveAlias(finding.Rule) != "forbidden-suppression" &&
			slices.ContainsFunc(allowedRules, func(name string) bool {
				return processor.RuleSet.ResolveAlias(name) == processor.RuleSet.ResolveAlias(finding.Rule)
			}) && policy.ReportFor(finding.Rule) == configuration.SuppressionsAllowed {
			finding.Source.Suppression = &results.SuppressionOrigin{Path: targetPath, Lines: lines}
		} else {
			finding.Source.Suppression = nil
		}
		if applies && evaluation.Suppresses(finding.Rule, lines) {
			continue
		}
		filtered = append(filtered, finding)
	}
	for _, d := range reports {
		filtered = append(filtered, finding.FromDiagnostic(d))
	}
	if audit != nil {
		for index, directive := range directives {
			for _, name := range directive.Rules {
				if !used[selector{index, name}] && !evaluation.Suppresses("unused-suppression", sourcetext.LineRange{First: directive.Position.Line, Last: directive.Position.Line}) {
					finding := finding.FromDiagnostic(results.Diagnostic{
						Rule: "unused-suppression", Start: directive.Position,
						Message: fmt.Sprintf("inline suppression of %q did not match any findings", name),
					})
					finding.UnusedSuppressionRule = name
					filtered = append(filtered, finding)
				}
			}
		}
	}
	return filtered
}

func (processor Processor) filterForeignInline(path string, source []byte, findings []*finding.Finding, policy *configuration.SuppressionsPolicy, audit *inlineAudit, additional ...string) []*finding.Finding {
	local := make([]*finding.Finding, len(findings))
	for i, finding := range findings {
		scoped := *finding
		if scoped.SuppressionOrigin == nil {
			scoped.Source.Content = nil
		}
		local[i] = &scoped
	}
	local = processor.filterInlineSuppressionsAt(source, local, policy, path, audit, additional...)
	for _, finding := range local {
		if finding.SuppressionOrigin == nil {
			finding.Source.Content = source
		}
		if finding.Source.Suppression != nil && finding.Source.Suppression.Path == "" {
			finding.Source.Suppression.Path = path
		}
		if finding.Path == "" {
			finding.Path = path
		}
	}
	return local
}
