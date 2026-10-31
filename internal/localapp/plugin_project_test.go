package localapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestPluginProjectDeclarationsAuthorizeLoading(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := project.SelectedLock(); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("legacy lock silently activated: %v", err)
	}
	refs, err := project.Dependencies(true)
	if err != nil || len(refs) != 1 || refs[0].Name() != pin.Name {
		t.Fatalf("migration: %v %v", refs, err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	project, err = OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedLock()
	if err != nil || len(selected.Plugins) != 1 || selected.Plugins[0] != pin {
		t.Fatalf("migration changed pin: %+v %v", selected, err)
	}
	declared := project.Manifest.References[0]
	project.Manifest.References[0], err = pluginref.New(declared.Name(), "v2.0.0", declared.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.SelectedLock(); err == nil || !strings.Contains(err.Error(), "plugin install") {
		t.Fatalf("changed declaration accepted stale pin: %v", err)
	}
	if err := os.WriteFile(project.Manifest.Path, []byte("plugins: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	sources, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Repository})
	if err != nil || len(sources.Paths) != 0 || len(registry.Requests()) != requests {
		t.Fatalf("undeclared plugin activated: %+v %v", sources, err)
	}
}

func TestDirectDeclarationRequiresNoRegistryLock(t *testing.T) {
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.Parse("https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	project, err = OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedLock()
	if err != nil || len(selected.Plugins) != 0 {
		t.Fatalf("direct source acquired a registry pin: %+v %v", selected, err)
	}
	if got := project.DirectSources(); len(got) != 1 || got[0] != ref.DirectSource() {
		t.Fatalf("direct source lost: %v", got)
	}
	if project.Lock.Exists() {
		t.Fatal("created an unnecessary lockfile for an already pinned URL")
	}
}

func TestPluginProjectMissingPinsAndForeignScope(t *testing.T) {
	root, foreign := pluginRepository(t), pluginRepository(t)
	if err := os.MkdirAll(filepath.Join(foreign, ".github"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, ".github", "actionlint.yaml"), []byte("plugins: [owner/plugin]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := application.Input{Kind: application.Repository}
	if _, err := (PluginSelection{}).Resolve(t.Context(), foreign, input); err == nil || !strings.Contains(err.Error(), "plugin install") {
		t.Fatalf("missing lock ignored: %v", err)
	}
	sources, err := (PluginSelection{}).Resolve(t.Context(), root, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := sources.ValidateScope(t.Context(), []string{filepath.Join(foreign, "workflow.yml")}); err == nil {
		t.Fatal("foreign declarations ignored by language server")
	}
}

func TestPluginProjectPreservesConcurrentConfigEdit(t *testing.T) {
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
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
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := OpenPluginProject(t.Context(), pluginRepository(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	pin.Registry = "https://CATALOGUE.example/"
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), project.Lock.Lock); err != nil {
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
	if len(registry.Requests()) != requests || project.Lock.Lock.Plugins[0] != pin {
		t.Fatal("migration changed the locked pin or contacted the registry")
	}
}

func TestPluginProjectRollsBackLockWhenConfigIsBusy(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent lock", true: "existing lock"}[existing], func(t *testing.T) {
			root := pluginRepository(t)
			directory := filepath.Join(root, ".github")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("{\"schema_version\":1,\"plugins\":[]}\n")
			lockPath := filepath.Join(directory, pluginregistry.LockName)
			if existing {
				if err := os.WriteFile(lockPath, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			project, err := OpenPluginProject(t.Context(), root, "", "")
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
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := OpenPluginProject(t.Context(), pluginRepository(t), "", "")
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
	registry := testutil.NewPluginRegistry(t)
	registry.AddArtifact(t, "v1.0.0", "plugin.wasm", []byte{0, 97, 115, 109, 1, 0, 0, 0})
	registry.Publish(t, "v1.0.0")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(testutil.FixturePluginName, "latest", testutil.FixtureRegistryURL)
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
	project, err = OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	if err := project.Resolve(t.Context(), refs, nil); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != requests || project.Lock.Lock.Plugins[0] != original {
		t.Fatal("install upgraded an existing matching pin")
	}
	// A failed migration must leave the previous on-disk lock usable.
	artifactURL := "https://" + testutil.FixturePluginName + "/releases/download/v2.0.0/plugin.component.wasm"
	good := registry.File(artifactURL)
	registry.SetFile(artifactURL, []byte("corrupt"))
	if err := project.Resolve(t.Context(), refs, refs); err == nil {
		t.Fatal("accepted corrupted replacement")
	}
	project, err = OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedLock()
	if err != nil || len(selected.Plugins) != 1 || selected.Plugins[0] != original {
		t.Fatalf("failed update damaged previous selection: %+v %v", selected, err)
	}
	registry.SetFile(artifactURL, good)
	if err := project.Resolve(t.Context(), refs, refs); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	selected, err = project.SelectedLock()
	if err != nil || len(selected.Plugins) != 1 || selected.Plugins[0].Version != "v2.0.0" || selected.Plugins[0].Asset != "plugin.component.wasm" {
		t.Fatalf("explicit update did not select component: %+v %v", selected, err)
	}
}
