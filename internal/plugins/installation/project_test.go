package installation_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestPluginProjectPreservesConcurrentConfigEdit(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), plugininstall.Acquisition{}, root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{}); err != nil {
		t.Fatal(err)
	}
	// Seed an existing lock so this checks rollback of an already tracked file.
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(project.Lock.Path)
	if err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("# user edit\nplugins: []\n")
	if err := os.WriteFile(project.Manifest.Path, concurrent, 0600); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{}); err == nil {
		t.Fatal("overwrote concurrent config edit")
	}
	after, err := os.ReadFile(project.Lock.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("lock changed after failed save: %s %v", after, err)
	}
	config, err := os.ReadFile(project.Manifest.Path)
	if err != nil || !bytes.Equal(config, concurrent) {
		t.Fatalf("lost user edit: %s %v", config, err)
	}
}

func TestPluginProjectMigratesRegistryCasingOffline(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), fixtureAcquisition(registry), repositoryfixture.Directory(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	pin.Registry = "https://CATALOGUE.example/"
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := plugininstall.Sync(t.Context(), project.Lock.Lock, fixtureAcquisition(registry)); err != nil {
		t.Fatal(err)
	}
	refs, err := project.Dependencies(true)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	if err := project.Resolve(t.Context(), refs, nil); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != requests || !project.Lock.Lock.Plugins[0].SameRelease(pin) {
		t.Fatal("migration changed the locked pin or contacted the registry")
	}
}

func TestPluginProjectRollsBackLockWhenConfigIsBusy(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent lock", true: "existing lock"}[existing], func(t *testing.T) {
			t.Parallel()
			root := repositoryfixture.Directory(t)
			directory := filepath.Join(root, ".github")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("{\"schema_version\":1,\"plugins\":[]}\n")
			lockPath := filepath.Join(directory, plugininstall.LockName)
			if existing {
				if err := os.WriteFile(lockPath, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), plugininstall.Acquisition{}, root, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(project.Manifest.Path+".writing", 0700); err != nil {
				t.Fatal(err)
			}
			if err := project.Save(t.Context(), []pluginref.Reference{}); err == nil {
				t.Fatal("busy config accepted")
			}
			data, err := os.ReadFile(lockPath)
			if existing && (err != nil || !bytes.Equal(data, original)) {
				t.Fatalf("original lock not restored: %s %v", data, err)
			}
			if !existing && !os.IsNotExist(err) {
				t.Fatalf("lock left after failed config write: %v", err)
			}
			if _, err := os.Stat(project.Manifest.Path); !os.IsNotExist(err) {
				t.Fatalf("config created despite busy guard: %v", err)
			}
		})
	}
}

func TestPluginProjectReplacesRemovedPackageWithSamePluginID(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), fixtureAcquisition(registry), repositoryfixture.Directory(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	old := pin
	old.Name = "github.com/publisher/previous"
	old.URL = strings.Replace(pin.URL, "/plugin/", "/previous/", 1)
	if err := project.Lock.Set(old); err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(pin.Name, pin.Selector, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{ref}, nil); err != nil {
		t.Fatalf("removed package blocked its replacement: %v", err)
	}
	if len(project.Lock.Lock.Plugins) != 1 || project.Lock.Lock.Plugins[0] != pin {
		t.Fatalf("unexpected pins: %+v", project.Lock.Lock.Plugins)
	}
}

func TestPluginProjectUpdatesTransportOnlyExplicitly(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.AddArtifact(t, "v1.0.0", "plugin.wasm", []byte{0, 97, 115, 109, 1, 0, 0, 0})
	registry.Publish(t, "v1.0.0")
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: registry.Connections()}
	root := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(fixtureregistry.FixturePluginName, "latest", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginref.Reference{ref}
	if err := project.Resolve(t.Context(), refs, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	original := project.Lock.Lock.Plugins[0]
	registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	registry.Publish(t, "v1.0.0", "v2.0.0")
	project, err = plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	if err := project.Resolve(t.Context(), project.Manifest.References, nil); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != requests || !project.Lock.Lock.Plugins[0].SameRelease(original) {
		t.Fatal("install upgraded an existing matching pin")
	}
	// A failed migration must preserve both the live selection and saved config.
	artifactURL := "https://" + fixtureregistry.FixturePluginName + "/releases/download/v2.0.0/plugin.component.wasm"
	good := registry.File(artifactURL)
	registry.SetFile(artifactURL, []byte("corrupt"))
	if err := project.Resolve(t.Context(), refs, refs); err == nil {
		t.Fatal("accepted corrupted replacement")
	}
	if len(project.Lock.Lock.Plugins) != 1 || !project.Lock.Lock.Plugins[0].SameRelease(original) {
		t.Fatal("failed update changed the live selection")
	}
	project, err = plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedPlugins(t.Context())
	if err != nil || len(selected.Plugins) != 1 || !selected.Plugins[0].SameRelease(original) {
		t.Fatalf("failed update damaged previous selection: %+v %v", selected, err)
	}
	registry.SetFile(artifactURL, good)
	if err := project.Resolve(t.Context(), refs, refs); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	selected, err = project.SelectedPlugins(t.Context())
	if err != nil || len(selected.Plugins) != 1 || selected.Plugins[0].Version != "v2.0.0" || selected.Plugins[0].Asset != "plugin.component.wasm" {
		t.Fatalf("explicit update did not select component: %+v %v", selected, err)
	}
}
