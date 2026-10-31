package localapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestMigrationDefaultsAndAtomicFailure(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# source comment\nconfig-variables: []\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Changed || !bytes.Contains(plan.Content, []byte("shellcheck")) || !bytes.Contains(plan.Content, []byte("ruff")) {
		t.Fatalf("missing defaults: %s", plan.Content)
	}
	registry.SetOffline(true)
	if err := plan.Apply(t.Context()); err == nil {
		t.Fatal("offline default resolution succeeded")
	}
	if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("failed migration changed config: %s %v", actual, err)
	}
	lockPath := filepath.Join(filepath.Dir(path), pluginregistry.LockName)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("failed migration wrote lock: %v", err)
	}
	registry.SetOffline(false)
	if err := plan.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || len(cfg.Plugins) != 2 || cfg.ConfigVariables == nil {
		t.Fatalf("migrated config: %+v %v", cfg, err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil || len(lock.Lock.Plugins) != 2 {
		t.Fatalf("default pins: %+v %v", lock, err)
	}
	registry.SetOffline(true)
	before := len(registry.Requests())
	if err := MigrateConfig(t.Context(), path, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}}); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != before {
		t.Fatal("already migrated config contacted registry")
	}
}

func TestConfigMigrationPreservesLockOnlyPlugins(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("config-variables: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), project.Lock.Lock); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)

	if err := MigrateConfig(t.Context(), path, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}}); err != nil {
		t.Fatal(err)
	}
	migrated, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.Plugins) != 1 || migrated.Plugins[0].Name() != pin.Name || migrated.Plugins[0].Version() != pin.Selector || migrated.Plugins[0].Registry() != pin.Registry {
		t.Fatalf("lock-only plugin selection changed: %+v, %v", migrated.Plugins, err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(filepath.Dir(path), pluginregistry.LockName))
	if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0] != pin {
		t.Fatalf("lock-only pin changed: %+v, %v", lock, err)
	}
}

func TestConfigMigrationPreservesInheritedPluginList(t *testing.T) {
	root := pluginRepository(t)
	path := filepath.Join(root, "actionlint.yaml")
	base := filepath.Join(root, "base.yaml")
	if err := os.WriteFile(base, []byte("plugins: [publisher/base]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("extends: [base.yaml]\nconfig-variables: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plan.Content, []byte("plugins:")) {
		t.Fatalf("copied inherited plugins into leaf config:\n%s", plan.Content)
	}
	if err := plan.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, pluginregistry.LockName)); !os.IsNotExist(err) {
		t.Fatalf("migration wrote lock for inherited declaration: %v", err)
	}
	if err := os.WriteFile(base, []byte("plugins: [publisher/base, publisher/next]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Declared || len(manifest.References) != 2 || manifest.References[0].Name() != "github.com/publisher/base" || manifest.References[1].Name() != "github.com/publisher/next" {
		t.Fatalf("leaf stopped inheriting plugins: %+v", manifest.References)
	}
}

func TestAutomaticMigrationSkipsConfigUnderLinkedParent(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "repo")
	shared := filepath.Join(workspace, "shared")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(shared, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(shared, "actionlint.yaml")
	original := []byte("config-variables: []\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(root, ".github")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := (PluginSelection{}).Migrate(t.Context(), root, application.Input{Kind: application.Repository}, DefaultPluginOptions{}); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("automatic migration changed config outside repository: %q, %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(shared, pluginregistry.LockName)); !os.IsNotExist(err) {
		t.Fatalf("automatic migration wrote lock outside repository: %v", err)
	}
	invalid := []byte("config-variables: [\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if err := (PluginSelection{}).Migrate(t.Context(), root, application.Input{Kind: application.Repository}, DefaultPluginOptions{}); err == nil {
		t.Fatal("automatic migration skipped reading the linked config")
	}
	actual, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, invalid) {
		t.Fatalf("automatic migration changed invalid config outside repository: %q, %v", actual, err)
	}
}

func TestStdinMigrationMatchesPluginSelection(t *testing.T) {
	for _, tc := range []struct {
		name          string
		inputName     string
		absolute      bool
		exists        bool
		shouldMigrate bool
		wantError     bool
	}{
		{name: "unnamed", inputName: ""},
		{name: "stdin marker", inputName: "<stdin>"},
		{name: "missing relative", inputName: "missing.yml"},
		{name: "missing absolute", inputName: "missing.yml", absolute: true},
		{name: "invalid named path", inputName: "\x00", wantError: true},
		{name: "existing named", inputName: "workflow.yml", exists: true, shouldMigrate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pluginRepository(t)
			configDir := filepath.Join(root, ".github")
			if err := os.Mkdir(configDir, 0700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(configDir, "actionlint.yaml")
			original := []byte("config-variables: []\n")
			if err := os.WriteFile(configPath, original, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.exists {
				if err := os.WriteFile(filepath.Join(root, tc.inputName), []byte("on: push\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			inputName := tc.inputName
			if tc.absolute {
				inputName = filepath.Join(root, inputName)
			}
			input := application.Input{Kind: application.Stdin, Name: inputName}
			err := (PluginSelection{}).Migrate(t.Context(), root, input, DefaultPluginOptions{})
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid named stdin path was accepted")
				}
				return
			} else if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.shouldMigrate {
				if bytes.Equal(actual, original) {
					t.Fatal("existing named stdin path was not migrated")
				}
			} else if !bytes.Equal(actual, original) {
				t.Fatalf("unused stdin selection migrated project config: %q", actual)
			}
			if _, err := os.Stat(filepath.Join(configDir, pluginregistry.LockName)); !os.IsNotExist(err) {
				t.Fatalf("stdin migration unexpectedly wrote lock: %v", err)
			}
		})
	}
}

func TestDefaultPluginSelectionPreservesChoices(t *testing.T) {
	for _, tc := range []struct{ config, want string }{
		{"{}", "ruff,shellcheck"},
		{"plugins: []", ""},
		{"plugins: null", ""},
		{"plugins: [owner/custom]", "github.com/owner/custom"},
		{"tools: {ruff: false}", "shellcheck"},
		{"lint: {rules: {external: {shellcheck: off}}}", "ruff"},
		{"tools: {ruff: {backend: {process: {executable: custom-ruff}}}}", "shellcheck"},
		{"tools: {shellcheck: {config: .shellcheckrc}}", "ruff,shellcheck"},
		{"tools: {shellcheck: {config: {external-sources: true}}}", "ruff,shellcheck"},
		{"tools: {shellcheck: {config: {source-path: [scripts]}}}", "ruff,shellcheck"},
	} {
		t.Run(tc.config, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			refs, err := DefaultPluginReferences(cfg, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, ref := range refs {
				names = append(names, ref.String())
			}
			if strings.Join(names, ",") != tc.want {
				t.Fatalf("%v, want %s", names, tc.want)
			}
		})
	}
}

func TestConfigMigrationRejectsChangedCandidate(t *testing.T) {
	for _, candidate := range []string{
		"plugins: []\nlint: {rules: {correctness: {runner-label: {options: {unknown: true}}}}}\n",
		"plugins: []\nextends: [new-base.yaml]\n",
	} {
		t.Run(candidate, func(t *testing.T) {
			root := pluginRepository(t)
			path := filepath.Join(root, "actionlint.yaml")
			original := []byte("plugins: []\nconfig-variables: []\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
			if err != nil {
				t.Fatal(err)
			}
			plan.Content = []byte(candidate)
			plan.Changed = false // Apply must recompute state from validated bytes.
			if err := plan.Apply(t.Context()); err == nil {
				t.Fatal("changed invalid migration candidate accepted")
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("invalid candidate changed config: %q, %v", actual, err)
			}
			if _, err := os.Stat(plan.project.Lock.Path); !os.IsNotExist(err) {
				t.Fatalf("invalid candidate wrote lock: %v", err)
			}
		})
	}
}

func TestConfigMigrationRejectsConcurrentSourceEdits(t *testing.T) {
	for _, editBase := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "inherited"}[editBase], func(t *testing.T) {
			root := pluginRepository(t)
			path := filepath.Join(root, "actionlint.yaml")
			base := filepath.Join(root, "base.yaml")
			original := []byte("extends: [base.yaml]\nconfig-variables: []\n")
			if err := os.WriteFile(base, []byte("plugins: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
			if err != nil {
				t.Fatal(err)
			}
			edited := path
			if editBase {
				edited = base
			}
			userEdit := []byte("plugins: []\nconfig-variables: [USER_EDIT]\n")
			if err := os.WriteFile(edited, userEdit, 0600); err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") {
				t.Fatalf("concurrent edit accepted: %v", err)
			}
			actual, err := os.ReadFile(edited)
			if err != nil || !bytes.Equal(actual, userEdit) {
				t.Fatalf("user edit lost: %q, %v", actual, err)
			}
			if _, err := os.Stat(plan.project.Lock.Path); !os.IsNotExist(err) {
				t.Fatalf("stale migration wrote lock: %v", err)
			}
			if editBase {
				actual, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(actual, original) {
					t.Fatalf("stale migration changed selected file: %q, %v", actual, err)
				}
			}
		})
	}
}

func TestInitializeConfigInstallsDefaultPins(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	root := pluginRepository(t)
	path, err := InitializeConfig(t.Context(), root, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || len(cfg.Plugins) != 2 {
		t.Fatalf("defaults missing: %+v %v", cfg, err)
	}
	registry.SetOffline(true)
	if _, err := InitializeConfig(t.Context(), root, "2.0.0"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing config not protected: %v", err)
	}
}

func TestInitializeConfigPreservesLockOnlySelection(t *testing.T) {
	for _, name := range []string{testutil.FixturePluginName, "ruff"} {
		t.Run(name, func(t *testing.T) {
			registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
			registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
			url := testutil.FixtureRegistryURL
			if name == "ruff" {
				url = pluginregistry.DefaultURL
			}
			client, err := pluginregistry.New(url)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), name, "v1.0.0")
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
			path, err := InitializeConfig(t.Context(), root, "2.0.0")
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != name || cfg.Plugins[0].Version() != pin.Selector || cfg.Plugins[0].Registry() != pin.Registry {
				t.Fatalf("locked selection changed: %+v, %v", cfg, err)
			}
			lock, err := pluginregistry.OpenLock(t.Context(), project.Lock.Path)
			if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0] != pin {
				t.Fatalf("locked bytes changed: %+v, %v", lock, err)
			}
		})
	}
}
