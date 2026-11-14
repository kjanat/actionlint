package policy

import (
	"fmt"

	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func (processor Processor) lifecycleConfiguration(p Policy, sourceConfig *repoconfig.Config, sourcePath, rule string, compositeConfigs map[string]*repoconfig.Config) *repoconfig.Config {
	if p.Config != nil && p.Config.Lint.Enabled != nil && !*p.Config.Lint.Enabled {
		return p.Config
	}
	descriptor, _ := processor.RuleSet.FindRule(rule)
	switch descriptor.Scope {
	case rulescope.SourceConfiguration:
		return sourceConfig
	case rulescope.CompositeConfiguration:
		if config, ok := compositeConfigs[sourcePath]; ok {
			return config
		}
	case rulescope.CallerConfiguration:
	}
	return p.Config
}

// assessLifecycle separates required findings before project suppressions run.
func (processor Processor) assessLifecycle(p Policy, findings []*findingmodel.Finding, sources map[string][]byte, compositeConfigs map[string]*repoconfig.Config) ([]*findingmodel.Finding, []*findingmodel.Finding, error) {
	optional := make([]*findingmodel.Finding, 0, len(findings))
	var required []*findingmodel.Finding
	for _, finding := range findings {
		evidence := finding.LifecycleEvidence
		if evidence == nil {
			optional = append(optional, finding)
			continue
		}
		sourceConfig := p.Config
		if _, foreign := sources[finding.Path]; foreign {
			var prepared bool
			sourceConfig, prepared = p.SourceConfigurations[finding.Path]
			if !prepared {
				return nil, nil, fmt.Errorf("lifecycle configuration was not prepared for source %q", finding.Path)
			}
		}
		config := processor.lifecycleConfiguration(p, sourceConfig, finding.Path, finding.Rule, compositeConfigs)
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
			if evidence.Current && (!project.Matched || evidence.Effective.IsZero()) {
				optional = append(optional, finding)
			}
			continue
		}
		severity := lifecycle.Severity("")
		if projectActive {
			severity = project.Severity
		}
		if level, fixed := processor.RuleSet.FixedDiagnosticLevel(config, finding.Rule); fixed {
			switch level {
			case "info":
				severity = lifecycle.Info
			case "warn":
				severity = lifecycle.Warning
			case "error":
				severity = lifecycle.Error
			}
		}
		if mandatoryActive && mandatory.Severity.Rank() > severity.Rank() {
			severity = mandatory.Severity
		}
		finding.Severity = string(severity)
		finding.Lifecycle = lifecycleReport(*evidence, project.Active, mandatory.Active)
		if mandatoryActive {
			finding.RequiredLifecycle = true
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
				Severity: string(decision.Severity), StartsOn: decision.StartsOn.String(), Enforced: enforced,
			})
		}
	}
	appendDecisions(optional, false)
	appendDecisions(required, true)
	return report
}
