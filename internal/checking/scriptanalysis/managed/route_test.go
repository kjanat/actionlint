package managed

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

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

var shellcheckRoute = func() documentRoute {
	transform, err := integrationconfig.New(shellcheckManifest, bundledproviders.Program)
	if err != nil {
		panic(err)
	}
	return documentRoute{name: shellcheckManifest.Rule.Name, capabilities: shellcheckManifest.Capabilities, settingsResolver: transform}
}()
