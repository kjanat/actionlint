package workspace_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestNetworkPolicyAppliesWithoutProjectConfiguration(t *testing.T) {
	root := t.TempDir()
	policy := filepath.Join(root, "network-policy.toml")
	if err := os.WriteFile(policy, []byte("[restrictions.network.metadata-mirrors]\nexclude-services = ['jsdelivr']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_NETWORK_POLICY", policy)
	rules := checking.ConfigurationRules()
	for _, disabled := range []bool{false, true} {
		session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: rules, SkipProjectConfig: disabled, ReadFile: func(path string) ([]byte, error) {
			if path == policy {
				return []byte("[restrictions.network.metadata-mirrors]\nexclude-services = []\n"), nil
			}
			return os.ReadFile(path)
		}})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := session.ConfigForProject(nil)
		if err != nil || cfg.MetadataMirrorAccess() != references.NoMirrors {
			t.Fatalf("skip project = %t: access = %v, error = %v", disabled, cfg.MetadataMirrorAccess(), err)
		}
		selection, err := session.ReadStdin(strings.NewReader("on: push\njobs: {}\n"), false)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(selection.Inputs, policy) {
			t.Fatalf("policy absent from analysis dependencies: %v", selection.Inputs)
		}
	}
	cfg, err := rules.Parse([]byte(`{"network":{"metadata-mirrors":{"services":["jsdelivr"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: rules, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ConfigForProject(nil); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("programmatic configuration bypassed restriction: %v", err)
	}
	if err := os.Remove(policy); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: rules}); err == nil {
		t.Fatal("missing required policy was ignored")
	}
}

func TestProjectOverlayCannotEnableForbiddenMirror(t *testing.T) {
	root := t.TempDir()
	policy := filepath.Join(root, "network-policy.toml")
	if err := os.WriteFile(policy, []byte("[restrictions.network.metadata-mirrors]\nrequire-public = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_NETWORK_POLICY", policy)
	rules := checking.ConfigurationRules()
	overlay, err := rules.ParseOverlay("config", []byte(`{"network":{"metadata-mirrors":{"allow-unknown-repositories":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: rules, ConfigOverlays: []config.Overlay{overlay}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ConfigForProject(nil); err == nil || !strings.Contains(err.Error(), "require-public") {
		t.Fatalf("overlay bypassed restriction: %v", err)
	}
}

func TestAdditionalPolicyCannotReplaceUserPolicy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	user, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(user, "actionlint")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "network-policy.toml"), []byte("[restrictions.network.metadata-mirrors]\nrequire-public = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "team.toml")
	if err := os.WriteFile(extra, []byte("[restrictions.network.metadata-mirrors]\nrequire-public = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_NETWORK_POLICY", extra)
	rules := checking.ConfigurationRules()
	cfg, err := rules.Parse([]byte(`{"network":{"metadata-mirrors":{"allow-unknown-repositories":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: rules, Config: cfg, SkipProjectConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ConfigForProject(nil); err == nil || !strings.Contains(err.Error(), "network-policy.toml") {
		t.Fatalf("additional policy erased user restriction: %v", err)
	}
}
