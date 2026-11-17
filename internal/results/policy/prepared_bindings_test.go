package policy_test

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

// preparePolicy mirrors the host's per-entry preparation and seals it before publication.
func preparePolicy(rules configuration.RuleSet, caller *configuration.Config, p policy.DocumentInputs, sources map[string]*configuration.Config, findings []*finding.Finding) policy.DocumentInputs {
	registry := rulebinding.NewRegistry(rules)
	p.Scope = registry.Prepare(caller, caller, caller)
	p.SourceBindings = make(map[string]*rulebinding.Scope, len(sources))
	for path, source := range sources {
		p.SourceBindings[path] = registry.Prepare(caller, source, source)
	}
	for _, item := range findings {
		scope := p.Scope
		if selected, ok := p.SourceBindings[item.Path]; ok {
			scope = selected
		}
		item.Binding = scope.Rule(item.Rule).ID()
	}
	p.Bindings = registry.Seal()
	return p
}

func publishFindings(processor policy.Processor, p policy.DocumentInputs, findings []*finding.Finding, sources map[string][]byte) ([]*finding.Finding, error) {
	published, err := policy.Publish([]policy.Batch{{Processor: processor, Policy: p, InputPath: p.ConfigPath, Findings: findings, Sources: sources}}, nil)
	return published.Findings[0], err
}
