package policy

import (
	"encoding/json/v2"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// assessLifecycle separates required findings before project suppressions run.
func (processor Processor) assessLifecycle(p Policy, findings []*finding.Finding, authority map[*finding.Finding]obligation) ([]*finding.Finding, []*finding.Finding, error) {
	optional := make([]*finding.Finding, 0, len(findings))
	var required []*finding.Finding
	for _, finding := range findings {
		evidence := finding.LifecycleEvidence
		if evidence == nil {
			optional = append(optional, finding)
			continue
		}
		prepared, exists := p.Bindings.Lookup(finding.Binding)
		if !exists {
			return nil, nil, fmt.Errorf("finding %q has no prepared configuration binding", finding.Rule)
		}
		config := prepared.Policy()
		policies, err := config.LifecyclePolicies()
		if err != nil {
			return nil, nil, err
		}
		project := lifecycle.Assess(*evidence, policies)
		if processor.RuleSet.DiagnosticLevel(config, finding.Rule) == "off" {
			project = lifecycle.Assessment{Matched: true}
		}
		mandatory := lifecycle.Assess(*evidence, p.EnforcedLifecycle)
		projectActive := len(project.Active) != 0
		mandatoryActive := len(mandatory.Active) != 0
		if !projectActive && !mandatoryActive {
			if evidence.Current && (evidence.Kind == lifecycle.SupportStarts || !project.Matched || evidence.Effective.IsZero()) {
				optional = append(optional, finding)
			}
			continue
		}
		var level severity.Severity
		if projectActive {
			level = project.Severity
		}
		if configured, fixed := processor.RuleSet.FixedDiagnosticLevel(config, finding.Rule); fixed {
			if selected, ok := configured.Severity(); ok {
				level = selected
			}
		}
		if mandatoryActive && mandatory.Severity.Rank() > level.Rank() {
			level = mandatory.Severity
		}
		finding.Severity = level
		finding.Lifecycle = lifecycleReport(*evidence, project.Active, mandatory.Active)
		if mandatoryActive {
			identity, err := json.Marshal(finding.Lifecycle)
			if err != nil {
				return nil, nil, err
			}
			authority[finding] = obligation{domain: "lifecycle", identity: string(identity)}
			finding.Source.Suppression = nil
			finding.NonBlocking = false
			required = append(required, finding)
		} else {
			optional = append(optional, finding)
		}
	}
	return optional, required, nil
}

func lifecycleReport(e lifecycle.Evidence, optional, required []lifecycle.Decision) *results.LifecycleReport {
	report := &results.LifecycleReport{
		Subject: string(e.Subject), Name: e.Name, Event: string(e.Kind), Source: e.Source,
		AnnouncedOn: e.Announced.String(), EffectiveOn: e.Effective.String(), AsOf: e.AsOf.String(),
	}
	appendDecisions := func(decisions []lifecycle.Decision, enforced bool) {
		for _, decision := range decisions {
			report.Policies = append(report.Policies, results.LifecyclePolicyReport{
				ID: decision.PolicyID, Description: decision.Description, Origin: decision.Origin,
				Severity: decision.Severity, StartsOn: decision.StartsOn.String(), Enforced: enforced,
			})
		}
	}
	appendDecisions(optional, false)
	appendDecisions(required, true)
	return report
}
