package checking

import (
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/results"
)

// ConfigurationRules returns the same read-only built-in and bundled provider
// rules to every caller, or only the built-in rules when an embedded provider
// manifest is invalid.
func ConfigurationRules() configuration.RuleSet {
	rules, _ := ruleCatalog()
	return rules
}

// LoadConfigurationRules returns the rule catalog that ConfigurationRules
// returns, or the error that makes the embedded provider manifests invalid.
func LoadConfigurationRules() (configuration.RuleSet, error) { return ruleCatalog() }

var ruleCatalog = sync.OnceValues(func() (configuration.RuleSet, error) {
	declarations := make([]rule.Declaration, 0, len(builtins))
	for _, builtin := range builtins {
		declarations = append(declarations, builtin.Clone())
	}
	builtinRules, err := configuration.NewRuleSet(declarations)
	if err != nil {
		return configuration.RuleSet{}, err
	}
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		return builtinRules, err
	}
	if err := configuration.BundledToolProvidersError(); err != nil {
		return builtinRules, err
	}
	for _, manifest := range manifests {
		scope, err := manifest.Scope.ConfigurationScope()
		if err != nil {
			return builtinRules, fmt.Errorf("bundled provider %s: %w", manifest.Rule.Name, err)
		}
		declarations = append(declarations, rule.Declaration{Descriptor: manifest.Rule, Scope: scope, SupportsActions: true})
	}
	rules, err := configuration.NewRuleSet(declarations)
	if err != nil {
		return builtinRules, err
	}
	return rules, nil
})

func ConfigurationRegistrations(analyzers []results.AnalyzerRegistration, providers []contract.Registration) []configuration.RuleRegistration {
	var registrations []configuration.RuleRegistration
	for _, analyzer := range analyzers {
		if results.IsNilAnalyzer(analyzer.Analyzer) {
			continue
		}
		registrations = append(registrations, configuration.RuleRegistration{Declaration: analyzer.Analyzer.Descriptor().Clone(), Options: slices.Clone(analyzer.Options)})
	}
	for _, provider := range providers {
		_, builtin := FindRule(provider.Name)
		if builtin && provider.ToolIntegration == nil && provider.ToolSettings == nil {
			continue
		}
		registration := configuration.RuleRegistration{Declaration: provider.Clone(), ToolSettings: provider.ToolSettings}
		if provider.ToolIntegration != nil && provider.ToolIntegration.Tool == provider.Name {
			registration.ReplacesTool = provider.ToolIntegration.Tool
		}
		if builtin && provider.ToolSettings != nil {
			registration.ReplacesTool = provider.Name
		}
		registrations = append(registrations, registration)
	}
	return registrations
}
