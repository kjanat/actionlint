package policy

import (
	"fmt"
	"slices"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
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

// FilterInline applies inline directives to findings owned by this source.
func (processor Processor) FilterInline(source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, additional ...string) []*findingmodel.Finding {
	return processor.filterInlineSuppressionsAt(source, findings, policy, "", nil, additional...)
}

type inlineAudit struct {
	level func(string) repoconfig.Level
	used  func(sourcepos.Position, string)
}

func (processor Processor) filterInlineSuppressionsAt(source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, scope string, audit *inlineAudit, additional ...string) []*findingmodel.Finding {
	allowedRules := processor.SuppressionRules(additional...)
	directives, invalid := suppression.Parse(source, allowedRules, processor.RuleSet.Names())
	evaluation, reports := suppression.Evaluate(directives, func(name string) suppression.Decision {
		mode := policy.ReportFor(processor.RuleSet.ResolveAlias(name))
		return suppression.Decision{
			Suppress: mode == repoconfig.SuppressionsAllowed || mode == repoconfig.ReportSuppression,
			Report:   mode == repoconfig.ReportSuppression || mode == repoconfig.ReportAll,
		}
	}, processor.RuleSet.Names())
	for _, d := range invalid {
		findings = append(findings, findingmodel.FromDiagnostic(d))
	}
	filtered := make([]*findingmodel.Finding, 0, len(findings)+len(reports))
	type selector struct {
		directive int
		rule      string
	}
	used := make(map[selector]bool)
	for _, finding := range findings {
		first, last, applies := finding.Start.Line, finding.Start.Line, finding.Source == nil
		targetPath := scope
		if origin := finding.SuppressionOrigin; origin != nil {
			first, last, applies = origin.First, origin.Last, origin.Path == scope
			targetPath = origin.Path
		}
		if audit != nil && applies && audit.level(finding.Rule) != "off" && finding.Rule != "unused-suppression" {
			for index, directive := range directives {
				for _, selected := range directive.Rules {
					if first > directive.Last || last < directive.First || processor.RuleSet.ResolveAlias(selected) != processor.RuleSet.ResolveAlias(finding.Rule) {
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
			}) && policy.ReportFor(finding.Rule) == repoconfig.SuppressionsAllowed {
			finding.SuppressionTarget = &findingmodel.SuppressionOrigin{Path: targetPath, First: first, Last: last}
		} else {
			finding.SuppressionTarget = nil
		}
		if applies && evaluation.Suppresses(finding.Rule, first, last) {
			continue
		}
		filtered = append(filtered, finding)
	}
	for _, d := range reports {
		filtered = append(filtered, findingmodel.FromDiagnostic(d))
	}
	if audit != nil {
		for index, directive := range directives {
			for _, name := range directive.Rules {
				if !used[selector{index, name}] && !evaluation.Suppresses("unused-suppression", directive.Position.Line, directive.Position.Line) {
					finding := findingmodel.FromDiagnostic(results.Diagnostic{
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

// FilterForeignInline applies suppression directives from the declaring YAML.
func (processor Processor) FilterForeignInline(path string, source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, additional ...string) []*findingmodel.Finding {
	return processor.filterForeignInline(path, source, findings, policy, nil, additional...)
}

func (processor Processor) filterForeignInline(path string, source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, audit *inlineAudit, additional ...string) []*findingmodel.Finding {
	local := make([]*findingmodel.Finding, len(findings))
	for i, finding := range findings {
		scoped := *finding
		if scoped.SuppressionOrigin == nil {
			scoped.Source = nil
		}
		local[i] = &scoped
	}
	local = processor.filterInlineSuppressionsAt(source, local, policy, path, audit, additional...)
	for _, finding := range local {
		if finding.SuppressionOrigin == nil {
			finding.Source = source
		}
		if finding.SuppressionTarget != nil && finding.SuppressionTarget.Path == "" {
			finding.SuppressionTarget.Path = path
		}
		if finding.Path == "" {
			finding.Path = path
		}
	}
	return local
}
