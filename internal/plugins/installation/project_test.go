package installation_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func fixtureAcquisition(r *fixtureregistry.PluginRegistry) pluginacquisition.Options {
	return pluginacquisition.Options{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
}

func TestPluginProjectPreservesConcurrentConfigEdit(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), pluginacquisition.Options{}, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginreference.Reference{}); err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("# user edit\nplugins: []\n")
	if err := os.WriteFile(project.Manifest.Path, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginreference.Reference{}); err == nil {
		t.Fatal("overwrote concurrent config edit")
	}
	config, err := os.ReadFile(project.Manifest.Path)
	if err != nil || !bytes.Equal(config, concurrent) {
		t.Fatalf("lost user edit: %s %v", config, err)
	}
}

func TestPluginProjectUsesEquivalentRegistryCasingOffline(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), fixtureAcquisition(registry), repositoryfixture.Directory(t), "")
	if err != nil {
		t.Fatal(err)
	}
	pin.Registry = "https://CATALOGUE.example/"
	project.Releases = pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{pin}}
	if err := pluginregistry.Sync(t.Context(), project.Releases, fixtureAcquisition(registry)); err != nil {
		t.Fatal(err)
	}
	ref, err := pluginreference.New(pin.Name, pin.Version, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginreference.Reference{ref}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	if err := project.Resolve(t.Context(), refs, nil); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != requests || !project.Releases.Plugins[0].SameRelease(pin) {
		t.Fatal("resolution changed the selected release or contacted the registry")
	}
}

func TestPluginProjectRejectsBusyConfiguration(t *testing.T) {
	t.Parallel()
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), pluginacquisition.Options{}, repositoryfixture.Directory(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.Manifest.Path+".writing", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginreference.Reference{}); err == nil {
		t.Fatal("busy config accepted")
	}
	if _, err := os.Stat(project.Manifest.Path); !os.IsNotExist(err) {
		t.Fatalf("config created despite busy guard: %v", err)
	}
}

func TestPluginProjectReplacesRemovedPackageWithSamePluginID(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), fixtureAcquisition(registry), repositoryfixture.Directory(t), "")
	if err != nil {
		t.Fatal(err)
	}
	old := pin
	old.Name = "github.com/publisher/previous"
	old.URL = strings.Replace(pin.URL, "/plugin/", "/previous/", 1)
	project.Releases = pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{old}}
	ref, err := pluginreference.New(pin.Name, pin.Selector, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginreference.Reference{ref}, nil); err != nil {
		t.Fatalf("removed package blocked its replacement: %v", err)
	}
	if len(project.Releases.Plugins) != 1 || project.Releases.Plugins[0] != pin {
		t.Fatalf("unexpected pins: %+v", project.Releases.Plugins)
	}
}

func TestPluginProjectUpdatesTransportOnlyExplicitly(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.AddArtifact(t, "v1.0.0", "plugin.wasm", []byte{0, 97, 115, 109, 1, 0, 0, 0})
	registry.Publish(t, "v1.0.0")
	acquisition := pluginacquisition.Options{UserCacheDir: t.TempDir(), Connections: registry.Connections()}
	root := repositoryfixture.Directory(t)
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginreference.New(fixtureregistry.FixturePluginName, "latest", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginreference.Reference{ref}
	if err := project.Resolve(t.Context(), refs, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	original := project.Releases.Plugins[0]
	registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	registry.Publish(t, "v1.0.0", "v2.0.0")
	project, err = installation.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	if err := project.Resolve(t.Context(), project.Manifest.References, nil); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != requests || !project.Releases.Plugins[0].SameRelease(original) {
		t.Fatal("install upgraded an existing matching pin")
	}
	// A failed migration must preserve both the live selection and saved config.
	artifactURL := "https://" + fixtureregistry.FixturePluginName + "/releases/download/v2.0.0/plugin.component.wasm"
	good := registry.File(artifactURL)
	registry.SetFile(artifactURL, []byte("corrupt"))
	if err := project.Resolve(t.Context(), refs, refs); err == nil {
		t.Fatal("accepted corrupted replacement")
	}
	if len(project.Releases.Plugins) != 1 || !project.Releases.Plugins[0].SameRelease(original) {
		t.Fatal("failed update changed the live selection")
	}
	project, err = installation.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedPlugins(t.Context(), project.Manifest.References)
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
	selected, err = project.SelectedPlugins(t.Context(), project.Manifest.References)
	if err != nil || len(selected.Plugins) != 1 || selected.Plugins[0].Version != "v2.0.0" || selected.Plugins[0].Asset != "plugin.component.wasm" {
		t.Fatalf("explicit update did not select component: %+v %v", selected, err)
	}
}
