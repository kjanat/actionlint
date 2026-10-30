// Package registry registers the tool providers bundled with actionlint. It
// names no tool: every bundled manifest is registered with the selection an
// entrypoint makes for its rule name.
package registry

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/managed"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
)

// Selections chooses how each bundled provider runs, keyed by its rule name.
// A provider without a selection is registered disabled.
type Selections map[string]backend.Selection

// Registrations registers every bundled provider in directory order, binds
// managed document analyzers to providers that name an integration
// contract and routes repository-selected engines. The bundled data is a
// build input that tests validate, so a defect in it panics, as does a
// selection naming no bundled provider.
func Registrations(selections Selections) []tools.Registration {
	registrations, err := bundledRegistrations(selections)
	if err != nil {
		panic(err)
	}
	return registrations
}

func bundledRegistrations(selections Selections) ([]tools.Registration, error) {
	manifests, err := provider.Bundled()
	if err != nil {
		return nil, err
	}
	var unknown []string
	for name := range selections {
		if !slices.ContainsFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) != 0 {
		slices.Sort(unknown)
		return nil, fmt.Errorf("no bundled tool provider is named %s", strings.Join(unknown, ", "))
	}
	registrations := make([]tools.Registration, 0, len(manifests))
	for _, manifest := range manifests {
		registration, err := backend.RegisterProvider(backend.Provider{Manifest: manifest, Programs: bundled.Program, Selection: selections[manifest.Rule.Name], Bind: managed.Bind})
		if err != nil {
			return nil, fmt.Errorf("bundled tool provider %s: %w", manifest.Rule.Name, err)
		}
		registrations = append(registrations, backend.Configure(registration))
	}
	return registrations, nil
}

// Defaults returns a selection for every bundled provider that runs the
// provider's own command from PATH. It panics if the bundled data is
// invalid.
func Defaults() Selections {
	manifests, err := provider.Bundled()
	if err != nil {
		panic(err)
	}
	selections := make(Selections, len(manifests))
	for _, manifest := range manifests {
		selections[manifest.Rule.Name] = backend.Selection{Command: manifest.Rule.Name}
	}
	return selections
}
