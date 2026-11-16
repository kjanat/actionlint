package installation_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestAliasRegistryRelocationPreservesPublisher(t *testing.T) {
	t.Parallel()
	for _, repositoryID := range []int{11, 99} {
		t.Run(map[int]string{11: "same publisher", 99: "changed publisher"}[repositoryID], func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
			project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), fixtureAcquisition(registry), repositoryfixture.Directory(t), "")
			if err != nil {
				t.Fatal(err)
			}
			ref, err := pluginreference.New("ruff", "v1.0.0", pluginregistry.DefaultURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Resolve(t.Context(), []pluginreference.Reference{ref}, nil); err != nil {
				t.Fatal(err)
			}
			original := project.Releases.Plugins[0]
			const mirror = "https://mirror.example/"
			registry.AddPlugin(t, fixtureregistry.PluginPublisher{
				BaseURL: mirror, Name: original.Name, Aliases: []string{"ruff"}, RepositoryID: repositoryID, PluginID: "ruff",
			}, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
			ref, err = pluginreference.New("ruff", "v1.0.0", mirror)
			if err != nil {
				t.Fatal(err)
			}
			err = project.Resolve(t.Context(), []pluginreference.Reference{ref}, nil)
			if repositoryID != 11 {
				if err == nil {
					t.Fatal("registry relocation accepted a changed publisher")
				}
				if len(project.Releases.Plugins) != 1 || project.Releases.Plugins[0] != original {
					t.Fatal("rejected relocation discarded original pin")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := original
			want.Registry = mirror
			if len(project.Releases.Plugins) != 1 || project.Releases.Plugins[0] != want {
				t.Fatalf("relocated pin: %+v", project.Releases)
			}
		})
	}
}
