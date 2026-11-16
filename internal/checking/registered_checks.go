package checking

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// registeredChecks holds the document analyzers and script tools one check
// adds to the native rules, and the rule set they extend the configuration
// rules into. A check builds it once with registerChecks; configuration
// loading, input selection and analysis all read the same value.
type registeredChecks struct {
	analyzers []checkinputs.Registration
	tools     []contract.Registration
	// registrations declares the analyzers' and tools' rules to configuration.
	registrations []configuration.RuleRegistration
	// rules is the configuration rule set extended with registrations.
	rules configuration.RuleSet
	// names lists the rule names of the analyzers followed by the tools.
	names []string
}

// registerChecks validates the analyzers and tools of one check and builds
// the rule set they extend. Analyzers must not be nil, tool names must be
// unique and must not name a native rule, and no added rule may reuse the
// name of another rule.
func registerChecks(analyzers []checkinputs.Registration, tools []contract.Registration) (*registeredChecks, error) {
	var extensions []rule.Descriptor
	var names []string
	for _, analyzer := range analyzers {
		if checkinputs.IsNilAnalyzer(analyzer.Analyzer) {
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
	rules, err := ConfigurationRules().WithRegisteredRules(registrations...)
	if err != nil {
		return nil, err
	}
	return &registeredChecks{analyzers: analyzers, tools: tools, registrations: registrations, rules: rules, names: names}, nil
}

// registeredNames lists the rule names that configuration may name in
// addition to the configuration rules.
func (c *registeredChecks) registeredNames() []string {
	names := make([]string, 0, len(c.registrations))
	for _, registration := range c.registrations {
		names = append(names, registration.Declaration.Name)
	}
	return names
}

// toolNames lists the rule names of the registered tool providers. Unlike
// names, it holds no document analyzer, so tools.<name> entries bind to tool
// providers only.
func (c *registeredChecks) toolNames() []string {
	names := make([]string, 0, len(c.tools))
	for _, tool := range c.tools {
		names = append(names, tool.Name)
	}
	return names
}
