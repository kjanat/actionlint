// Package registry registers the tool providers bundled with actionlint. It
// names no tool: every bundled manifest is registered with the selection an
// entrypoint makes for its rule name.
package registry

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/managed"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// Selections chooses how each bundled provider runs, keyed by its rule name.
// A provider without a selection is registered disabled.
type Selections map[string]backends.Selection

// Registrations registers every bundled provider in directory order, binds
// managed document analyzers to providers that name an integration
// contract and routes repository-selected engines. The bundled data is a
// build input that tests validate, so a defect in it panics, as does a
// selection naming no bundled provider.
func Registrations(selections Selections) []contract.Registration {
	registrations, err := bundledRegistrations(selections)
	if err != nil {
		panic(err)
	}
	return registrations
}

func bundledRegistrations(selections Selections) ([]contract.Registration, error) {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		return nil, err
	}
	var unknown []string
	for name := range selections {
		if !slices.ContainsFunc(manifests, func(m toolmanifest.Manifest) bool { return m.Rule.Name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) != 0 {
		slices.Sort(unknown)
		return nil, fmt.Errorf("no bundled tool provider is named %s", strings.Join(unknown, ", "))
	}
	registrations := make([]contract.Registration, 0, len(manifests))
	for _, manifest := range manifests {
		registration, err := backends.RegisterProvider(backends.Provider{Manifest: manifest, Programs: bundledproviders.Program, Selection: selections[manifest.Rule.Name], Bind: managed.Bind})
		if err != nil {
			return nil, fmt.Errorf("bundled tool provider %s: %w", manifest.Rule.Name, err)
		}
		registrations = append(registrations, backends.Configure(registration))
	}
	return registrations, nil
}

// IDs returns the rule names of the bundled providers in directory order.
// It panics if the bundled data is invalid.
func IDs() []string {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		panic(err)
	}
	ids := make([]string, len(manifests))
	for i, manifest := range manifests {
		ids[i] = manifest.Rule.Name
	}
	return ids
}

// Defaults returns a selection for every bundled provider that runs the
// provider's own command from PATH and skips the provider, with a warning,
// when that command cannot run. It panics if the bundled data is invalid.
func Defaults() Selections {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		panic(err)
	}
	selections := make(Selections, len(manifests))
	for _, manifest := range manifests {
		selections[manifest.Rule.Name] = backends.Selection{Command: manifest.Rule.Name, Process: &hostprocess.CommandOptions{Optional: true}}
	}
	return selections
}
