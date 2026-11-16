package checking

import (
	"fmt"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// ConfigurationRules supplies the declarations used by the native and editor entry points.
// The cached set has no mutation operations; each caller receives the same read-only values.
func ConfigurationRules() configuration.RuleSet { return configurationRules() }

// ConfigurationRegistrations declares the rules that analyzers and tool registrations add
// to the built-in rule set, so configuration can name and configure them.
func ConfigurationRegistrations(checkers []checkinputs.Registration, providers []contract.Registration) []configuration.RuleRegistration {
	var registrations []configuration.RuleRegistration
	for _, checker := range checkers {
		if checkinputs.IsNilChecker(checker.Checker) {
			continue
		}
		registrations = append(registrations, configuration.RuleRegistration{Declaration: checker.Checker.Descriptor().Clone(), Options: slices.Clone(checker.Options)})
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

var configurationRules = sync.OnceValue(func() configuration.RuleSet {
	declarations := BuiltinRuleDeclarations()
	for _, manifest := range bundledManifests() {
		scope, err := manifest.Scope.ConfigurationScope()
		if err != nil {
			panic(fmt.Errorf("bundled provider %s: %w", manifest.Rule.Name, err))
		}
		declarations = append(declarations, rule.Declaration{Descriptor: manifest.Rule, Scope: scope, SupportsActions: true})
	}
	rules, err := configuration.NewRuleSet(declarations)
	if err != nil {
		panic(err)
	}
	return rules
})
