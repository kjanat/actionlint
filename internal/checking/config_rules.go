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
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
)

// ConfigurationRules supplies the declarations used by the native and editor entry points.
// The cached set has no mutation operations; each caller receives the same read-only values.
func ConfigurationRules() repoconfig.RuleSet { return configurationRules() }

func configurationRegistrations(checkers []checkinputs.Registration, providers []tools.Registration) []repoconfig.RuleRegistration {
	var registrations []repoconfig.RuleRegistration
	for _, checker := range checkers {
		if validation.IsNil(checker.Checker) {
			continue
		}
		descriptor := checker.Checker.Descriptor()
		registrations = append(registrations, repoconfig.RuleRegistration{Descriptor: descriptor.Clone(), Scope: descriptor.Scope, SupportsActions: descriptor.SupportsActions, Options: slices.Clone(checker.Options)})
	}
	for _, provider := range providers {
		_, builtin := FindRule(provider.Descriptor.Name)
		if builtin && provider.ToolIntegration == nil && provider.ToolSettings == nil {
			continue
		}
		registration := repoconfig.RuleRegistration{Descriptor: provider.Descriptor.Clone(), Scope: provider.Scope, SupportsActions: provider.SupportsActions, ToolSettings: provider.ToolSettings}
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

var configurationRules = sync.OnceValue(func() repoconfig.RuleSet {
	declarations := BuiltinRuleDeclarations()
	for _, name := range bundled.Names() {
		data, found := bundled.Manifest(name)
		if !found {
			panic("missing bundled provider manifest: " + name)
		}
		var manifest struct {
			Rule  rulemeta.Descriptor `json:"rule"`
			Scope string              `json:"scope"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			panic(fmt.Errorf("bundled provider %s: %w", name, err))
		}
		declaration := rulemeta.Declaration{Descriptor: manifest.Rule, SupportsActions: true}
		switch manifest.Scope {
		case "source":
			declaration.Scope = rulemeta.SourceConfiguration
		case "composite":
			declaration.Scope = rulemeta.CompositeConfiguration
		default:
			panic(fmt.Sprintf("bundled provider %s declares unknown configuration scope %q", name, manifest.Scope))
		}
		declarations = append(declarations, declaration)
	}
	rules, err := repoconfig.NewRuleSet(declarations)
	if err != nil {
		panic(err)
	}
	return rules
})
