package managed

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// shellcheckManifest is the bundled ShellCheck provider.
var shellcheckManifest = func() provider.Manifest {
	manifests, err := provider.Bundled()
	if err != nil {
		panic(err)
	}
	index := slices.IndexFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == "shellcheck" })
	if index < 0 {
		panic("no bundled shellcheck provider")
	}
	return manifests[index]
}()

// shellcheckRoute negotiates the bundled ShellCheck provider's settings.
var shellcheckRoute = documentRoute{name: shellcheckManifest.Rule.Name, capabilities: shellcheckManifest.Capabilities}
