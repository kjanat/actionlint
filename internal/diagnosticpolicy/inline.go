package diagnosticpolicy

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/suppression"
)

// SuppressionRules returns the ordered rule catalogue for inline directives.
func SuppressionRules(additional ...string) []string {
	rules := repoconfig.Rules()
	names := make([]string, 0, len(rules)+len(additional))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return append(names, additional...)
}

// FilterInline applies inline directives to findings owned by this source.
func FilterInline(source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, additional ...string) []*findingmodel.Finding {
	return filterInlineSuppressionsAt(source, findings, policy, "", additional...)
}

func filterInlineSuppressionsAt(source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, scope string, additional ...string) []*findingmodel.Finding {
	directives, invalid := suppression.Parse(source, SuppressionRules(additional...))
	if len(directives) == 0 && len(invalid) == 0 {
		return findings
	}
	evaluation := suppression.Evaluate(directives, func(name string) suppression.Decision {
		mode := policy.ReportFor(name)
		return suppression.Decision{
			Suppress: mode == repoconfig.SuppressionsAllowed || mode == repoconfig.ReportSuppression,
			Report:   mode == repoconfig.ReportSuppression || mode == repoconfig.ReportAll,
		}
	})
	for _, d := range invalid {
		findings = append(findings, findingmodel.FromDiagnostic(d))
	}
	filtered := make([]*findingmodel.Finding, 0, len(findings)+len(evaluation.Diagnostics))
	for _, finding := range findings {
		first, last, applies := finding.Start.Line, finding.Start.Line, finding.Source == nil
		if origin := finding.SuppressionOrigin; origin != nil {
			first, last, applies = origin.First, origin.Last, origin.Path == scope
		}
		if applies && evaluation.Suppresses(finding.Rule, first, last) {
			continue
		}
		filtered = append(filtered, finding)
	}
	for _, d := range evaluation.Diagnostics {
		filtered = append(filtered, findingmodel.FromDiagnostic(d))
	}
	return filtered
}

// FilterForeignInline applies suppression directives from the declaring YAML.
func FilterForeignInline(path string, source []byte, findings []*findingmodel.Finding, policy *repoconfig.SuppressionsPolicy, additional ...string) []*findingmodel.Finding {
	local := make([]*findingmodel.Finding, len(findings))
	for i, finding := range findings {
		scoped := *finding
		if scoped.SuppressionOrigin == nil {
			scoped.Source = nil
		}
		local[i] = &scoped
	}
	local = filterInlineSuppressionsAt(source, local, policy, path, additional...)
	for _, finding := range local {
		if finding.SuppressionOrigin == nil {
			finding.Source = source
		}
		if finding.Path == "" {
			finding.Path = path
		}
	}
	return local
}
