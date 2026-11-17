package managed

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/integrationconfig"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// shellcheckManifest is the bundled ShellCheck provider.
var shellcheckManifest = func() toolmanifest.Manifest {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		panic(err)
	}
	index := slices.IndexFunc(manifests, func(m toolmanifest.Manifest) bool { return m.Rule.Name == "shellcheck" })
	if index < 0 {
		panic("no bundled shellcheck provider")
	}
	return manifests[index]
}()

// shellcheckRoute negotiates the bundled ShellCheck provider's settings.
var shellcheckRoute = func() documentRoute {
	transform, err := integrationconfig.New(shellcheckManifest, bundledproviders.Program)
	if err != nil {
		panic(err)
	}
	return documentRoute{name: shellcheckManifest.Rule.Name, capabilities: shellcheckManifest.Capabilities, settingsResolver: transform}
}()
