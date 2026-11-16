package checking

import (
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// ConfigurationRules returns the rule catalog: the built-in rules followed by
// the bundled tool providers' rules, in the order users see them. The catalog
// is built once per process and has no mutation operations; each caller
// receives the same read-only values. It panics when the embedded provider
// manifests are invalid, a build defect that LoadConfigurationRules returns as
// an error.
func ConfigurationRules() configuration.RuleSet {
	rules, err := LoadConfigurationRules()
	if err != nil {
		panic(err)
	}
	return rules
}

// LoadConfigurationRules returns the rule catalog that ConfigurationRules
// returns, or the error that makes the embedded provider manifests invalid.
func LoadConfigurationRules() (configuration.RuleSet, error) { return ruleCatalog() }

// ruleCatalog declares the bundled providers' rules after the built-in rules,
// once per process.
var ruleCatalog = sync.OnceValues(func() (configuration.RuleSet, error) {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		return configuration.RuleSet{}, err
	}
	declarations := make([]rule.Declaration, 0, len(builtins)+len(manifests))
	for _, builtin := range builtins {
		declarations = append(declarations, builtin.Clone())
	}
	for _, manifest := range manifests {
		scope, err := manifest.Scope.ConfigurationScope()
		if err != nil {
			return configuration.RuleSet{}, fmt.Errorf("bundled provider %s: %w", manifest.Rule.Name, err)
		}
		declarations = append(declarations, rule.Declaration{Descriptor: manifest.Rule, Scope: scope, SupportsActions: true})
	}
	return configuration.NewRuleSet(declarations)
})

// ConfigurationRegistrations declares the rules that analyzers and tool registrations add
// to the built-in rule set, so configuration can name and configure them.
func ConfigurationRegistrations(analyzers []checkinputs.Registration, providers []contract.Registration) []configuration.RuleRegistration {
	var registrations []configuration.RuleRegistration
	for _, analyzer := range analyzers {
		if checkinputs.IsNilAnalyzer(analyzer.Analyzer) {
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
