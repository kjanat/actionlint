package policy_test

import (
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

// preparePolicy mirrors the host's per-entry preparation and seals it before publication.
func preparePolicy(rules repoconfig.RuleSet, caller *repoconfig.Config, p policy.Policy, sources map[string]*repoconfig.Config, findings []*finding.Finding) policy.Policy {
	registry := binding.NewRegistry(rules)
	p.Scope = registry.Prepare(caller, caller, caller)
	p.SourceBindings = make(map[string]*binding.Scope, len(sources))
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

func publishFindings(processor policy.Processor, p policy.Policy, findings []*finding.Finding, sources map[string][]byte) ([]*finding.Finding, error) {
	published, err := policy.Publish([]policy.Batch{{Processor: processor, Policy: p, InputPath: p.ConfigPath, Findings: findings, Sources: sources}}, nil)
	return published.Findings[0], err
}
