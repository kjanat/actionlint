package localapp

import (
	"maps"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestAliasRegistryRelocationPreservesPublisher(t *testing.T) {
	for _, repositoryID := range []int{11, 99} {
		t.Run(map[int]string{11: "same publisher", 99: "changed publisher"}[repositoryID], func(t *testing.T) {
			registry := testutil.NewDefaultPluginRegistry(t)
			project, err := OpenPluginProject(t.Context(), pluginRepository(t), "", "")
			if err != nil {
				t.Fatal(err)
			}
			ref, err := pluginref.New("ruff", "v1.0.0", pluginregistry.DefaultURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Resolve(t.Context(), []pluginref.Reference{ref}, nil); err != nil {
				t.Fatal(err)
			}
			original := project.Lock.Lock.Plugins[0]
			const mirror = "https://mirror.example/"
			other := &testutil.PluginRegistry{
				Files:   map[string][]byte{mirror + "statuses.json": []byte(`{"schema_version":1,"releases":[]}`)},
				BaseURL: mirror, Name: original.Name, Aliases: []string{"ruff"}, RepositoryID: repositoryID, PluginID: "ruff",
			}
			other.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
			maps.Copy(registry.Files, other.Files)
			ref.Registry = mirror
			err = project.Resolve(t.Context(), []pluginref.Reference{ref}, nil)
			if repositoryID != 11 {
				if err == nil {
					t.Fatal("registry relocation accepted a changed publisher")
				}
				if len(project.Lock.Lock.Plugins) != 1 || project.Lock.Lock.Plugins[0] != original {
					t.Fatal("rejected relocation discarded original pin")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := original
			want.Registry = mirror
			if len(project.Lock.Lock.Plugins) != 1 || project.Lock.Lock.Plugins[0] != want {
				t.Fatalf("relocated pin: %+v", project.Lock.Lock)
			}
		})
	}
}
