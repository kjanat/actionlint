package checking

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

type registeredChecks struct {
	analyzers     []results.AnalyzerRegistration
	tools         []contract.Registration
	registrations []configuration.RuleRegistration
	rules         configuration.RuleSet
	names         []string
}

func registerChecks(analyzers []results.AnalyzerRegistration, tools []contract.Registration) (*registeredChecks, error) {
	catalog, err := LoadConfigurationRules()
	if err != nil {
		return nil, err
	}
	var extensions []rule.Descriptor
	var names []string
	for _, analyzer := range analyzers {
		if results.IsNilAnalyzer(analyzer.Analyzer) {
			return nil, errors.New("nil analyzer registration")
		}
		descriptor := analyzer.Analyzer.Descriptor().Descriptor
		extensions = append(extensions, descriptor)
		names = append(names, descriptor.Name)
	}
	seenTools := map[string]bool{}
	for _, tool := range tools {
		name := tool.Name
		if seenTools[name] {
			return nil, fmt.Errorf("duplicate script tool %q", name)
		}
		seenTools[name] = true
		if registration.HasRule(name) {
			return nil, fmt.Errorf("script tool %q conflicts with a built-in analyzer", name)
		}
		if _, builtIn := FindRule(name); !builtIn {
			extensions = append(extensions, tool.Descriptor)
		}
		names = append(names, name)
	}
	if _, err := CombineRuleDeclarations(extensions...); err != nil {
		return nil, err
	}
	registrations := ConfigurationRegistrations(analyzers, tools)
	rules, err := catalog.WithRegisteredRules(registrations...)
	if err != nil {
		return nil, err
	}
	return &registeredChecks{analyzers: analyzers, tools: tools, registrations: registrations, rules: rules, names: names}, nil
}

func (c *registeredChecks) registeredNames() []string {
	names := make([]string, 0, len(c.registrations))
	for _, registration := range c.registrations {
		names = append(names, registration.Declaration.Name)
	}
	return names
}

func (c *registeredChecks) toolNames() []string {
	names := make([]string, 0, len(c.tools))
	for _, tool := range c.tools {
		names = append(names, tool.Name)
	}
	return names
}
