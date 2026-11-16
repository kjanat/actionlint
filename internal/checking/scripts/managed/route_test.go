package managed

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"

	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// shellcheckManifest is the bundled ShellCheck provider.
var shellcheckManifest = func() pluginmanifests.Manifest {
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		panic(err)
	}
	index := slices.IndexFunc(manifests, func(m pluginmanifests.Manifest) bool { return m.Rule.Name == "shellcheck" })
	if index < 0 {
		panic("no bundled shellcheck provider")
	}
	return manifests[index]
}()

// shellcheckRoute negotiates the bundled ShellCheck provider's settings.
var shellcheckRoute = func() documentRoute {
	transform, err := configuration.New(shellcheckManifest, bundledproviders.Program)
	if err != nil {
		panic(err)
	}
	return documentRoute{name: shellcheckManifest.Rule.Name, capabilities: shellcheckManifest.Capabilities, settingsResolver: transform}
}()
