package checking

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
)

// ConfigurationRules supplies the declarations used by the native and editor entry points.
// The cached set has no mutation operations; each caller receives the same read-only values.
func ConfigurationRules() configuration.RuleSet { return configurationRules() }

func configurationRegistrations(checkers []checkinputs.Registration, providers []tools.Registration) []configuration.RuleRegistration {
	var registrations []configuration.RuleRegistration
	for _, checker := range checkers {
		if validation.IsNil(checker.Checker) {
			continue
		}
		descriptor := checker.Checker.Descriptor()
		registrations = append(registrations, configuration.RuleRegistration{Descriptor: descriptor.Clone(), Scope: descriptor.Scope, SupportsActions: descriptor.SupportsActions, Options: slices.Clone(checker.Options)})
	}
	for _, provider := range providers {
		_, builtin := FindRule(provider.Descriptor.Name)
		if builtin && provider.ToolIntegration == nil && provider.ToolSettings == nil {
			continue
		}
		registration := configuration.RuleRegistration{Descriptor: provider.Descriptor.Clone(), Scope: provider.Scope, SupportsActions: provider.SupportsActions, ToolSettings: provider.ToolSettings}
		if provider.ToolIntegration != nil && provider.ToolIntegration.Tool == provider.Descriptor.Name {
			registration.ReplacesTool = provider.ToolIntegration.Tool
		}
		if builtin && provider.ToolSettings != nil {
			registration.ReplacesTool = provider.Descriptor.Name
		}
		registrations = append(registrations, registration)
	}
	return registrations
}

var configurationRules = sync.OnceValue(func() configuration.RuleSet {
	declarations := BuiltinRuleDeclarations()
	for _, name := range bundled.Names() {
		data, found := bundled.Manifest(name)
		if !found {
			panic("missing bundled provider manifest: " + name)
		}
		var manifest struct {
			Rule  rulemeta.Descriptor `json:"rule"`
			Scope manifests.Scope     `json:"scope"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			panic(fmt.Errorf("bundled provider %s: %w", name, err))
		}
		scope, err := manifest.Scope.ConfigurationScope()
		if err != nil {
			panic(fmt.Errorf("bundled provider %s: %w", name, err))
		}
		declaration := rulemeta.Declaration{Descriptor: manifest.Rule, Scope: scope, SupportsActions: true}
		declarations = append(declarations, declaration)
	}
	rules, err := configuration.NewRuleSet(declarations)
	if err != nil {
		panic(err)
	}
	return rules
})
