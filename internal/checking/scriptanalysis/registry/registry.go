// Package registry registers the tool providers bundled with actionlint.
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

// ToolSelections without an entry for a provider leaves it registered disabled.
type ToolSelections map[string]backends.ToolSelection

// Registrations panics on invalid bundled data or a selection that names no bundled provider.
func Registrations(selections ToolSelections) []contract.Registration {
	registrations, err := bundledRegistrations(selections)
	if err != nil {
		panic(err)
	}
	return registrations
}

func bundledRegistrations(selections ToolSelections) ([]contract.Registration, error) {
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

// IDs panics if the bundled data is invalid.
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

// Defaults panics if the bundled data is invalid.
func Defaults() ToolSelections {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		panic(err)
	}
	selections := make(ToolSelections, len(manifests))
	for _, manifest := range manifests {
		selections[manifest.Rule.Name] = backends.ToolSelection{Command: manifest.Rule.Name, Process: &hostprocess.CommandOptions{Optional: true}}
	}
	return selections
}
