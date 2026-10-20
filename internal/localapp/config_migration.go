package localapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/pluginref"
	"actionlint.kjanat.dev/internal/projectpath"
	"actionlint.kjanat.dev/internal/tools/shellcheck"
)

// DefaultPluginOptions preserves invocation-level native selections during migration.
// Configured backends and explicit configuration disables take precedence.
type DefaultPluginOptions struct {
	Shellcheck bool
	Ruff       bool
	Warning    func(string)
}

// DefaultPluginReferences supplies the v2 defaults only when plugins was omitted.
// An explicit empty list is a durable opt-out, including through inheritance.
func DefaultPluginReferences(cfg *config.Config, options DefaultPluginOptions) ([]pluginref.Reference, error) {
	if cfg.Plugins != nil {
		return slices.Clone(cfg.Plugins), nil
	}
	refs := []pluginref.Reference{}
	for _, selected := range []struct {
		name    string
		enabled bool
	}{
		{"shellcheck", options.Shellcheck && shellcheck.SupportsDocumentPlugin(cfg) &&
			(cfg.Tools.Shellcheck.Enabled == nil || *cfg.Tools.Shellcheck.Enabled) && config.DiagnosticLevel(cfg, "shellcheck") != "off"},
		{"ruff", options.Ruff && cfg.Tools.Ruff.Backend == nil &&
			(cfg.Tools.Ruff.Enabled == nil || *cfg.Tools.Ruff.Enabled) && config.DiagnosticLevel(cfg, "ruff") != "off"},
	} {
		if !selected.enabled {
			continue
		}
		ref, err := pluginref.Parse(selected.name)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// ConfigMigration previews the syntax conversion and default plugin declarations.
// Applying it downloads and verifies all dependencies before replacing either file.
type ConfigMigration struct {
	*config.FileMigration
	project *PluginProject
	refs    []pluginref.Reference
}

func PrepareConfigMigration(ctx context.Context, path string, options DefaultPluginOptions) (*ConfigMigration, error) {
	migration, err := config.PrepareMigration(path)
	if err != nil {
		return nil, err
	}
	project, err := OpenPluginProject(ctx, filepath.Dir(migration.Path), "", migration.Path)
	if err != nil {
		return nil, err
	}
	if err := migration.CheckUnchanged(); err != nil {
		return nil, err
	}
	resolved, err := config.ResolveBeforePlugins(project.Manifest.node, nil)
	if err != nil {
		return nil, err
	}
	refs, err := DefaultPluginReferences(resolved.Config, options)
	if err != nil {
		return nil, err
	}
	content, err := editPluginManifest(migration.Path, migration.Content, refs)
	if err != nil {
		return nil, err
	}
	if err := migration.SetContent(content); err != nil {
		return nil, err
	}
	if !project.Manifest.Declared && slices.ContainsFunc(refs, func(ref pluginref.Reference) bool {
		return ref.Name == "shellcheck" && ref.Registry == pluginref.DefaultRegistry
	}) {
		migration.Notes = append(migration.Notes, "ShellCheck now uses the isolated Wasm plugin; implicit source-file following stops. Explicit native source/rc configuration retains the native backend.")
	}
	project.StatusOptions.Warning = options.Warning
	return &ConfigMigration{migration, project, refs}, nil
}

func (m *ConfigMigration) Apply(ctx context.Context) error {
	if err := m.SetContent(m.Content); err != nil {
		return err
	}
	if !m.Changed {
		return nil
	}
	if err := m.CheckUnchanged(); err != nil {
		return err
	}
	if err := m.project.Resolve(ctx, m.refs, nil); err != nil {
		return fmt.Errorf("config migration left %s unchanged: %w", m.Path, err)
	}
	if err := m.CheckUnchanged(); err != nil {
		return err
	}
	original := m.project.Manifest.original
	m.project.Manifest.original = bytes.Clone(m.Content)
	if err := m.project.Save(ctx, m.refs); err != nil {
		m.project.Manifest.original = original
		return err
	}
	return nil
}

// MigrateConfig automatically upgrades a selected legacy local file.
// Files already using current syntax are never implicitly enrolled in plugins.
func MigrateConfig(ctx context.Context, path string, options DefaultPluginOptions) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("could not read config file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Reading a linked config remains supported; automatic writes never
		// change its target outside the selected project.
		return nil
	}
	migration, err := config.PrepareMigration(path)
	if err != nil || !migration.Changed {
		return err
	}
	prepared, err := PrepareConfigMigration(ctx, path, options)
	if err != nil {
		return err
	}
	if err := prepared.Apply(ctx); err != nil {
		return err
	}
	if options.Warning != nil {
		options.Warning("migrated configuration: " + prepared.Path)
		for _, note := range prepared.Notes {
			options.Warning(note)
		}
	}
	return nil
}

// Migrate upgrades selected local configs before runtime plugin selection.
// Cache planning calls Select directly and remains read-only.
func (s PluginSelection) Migrate(ctx context.Context, directory string, input application.Input, options DefaultPluginOptions) error {
	if s.Disabled || len(s.Paths) != 0 || s.LockFile != "" {
		return nil
	}
	if s.ConfigFile != "" {
		return MigrateConfig(ctx, s.ConfigFile, options)
	}
	starts := []string{directory}
	if input.Kind == application.Files && len(input.Paths) != 0 {
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, filepath.Dir(path))
		}
	} else if input.Kind == application.Stdin && input.Name != "" && input.Name != "<stdin>" {
		path := input.Name
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		starts = []string{filepath.Dir(path)}
	}
	seen := map[string]bool{}
	for _, start := range starts {
		root := projectpath.RepositoryRoot(start)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		path, err := pluginConfigPath(filepath.Join(root, ".github"))
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := MigrateConfig(ctx, path, options); err != nil {
			return err
		}
	}
	return nil
}

// InitializeConfig verifies default plugin artifacts before creating the config
// and lockfile. A failed resolution leaves both project files untouched.
func InitializeConfig(ctx context.Context, root, version string) (string, error) {
	lock, err := OpenPluginLock(ctx, root, "", "")
	if err != nil {
		return "", err
	}
	path, err := pluginConfigPath(filepath.Dir(lock.Path))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("config file already exists at %q", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	project, err := OpenPluginProject(ctx, root, "", "")
	if err != nil {
		return "", err
	}
	if project.Manifest.Exists {
		return "", fmt.Errorf("config file already exists at %q", project.Manifest.Path)
	}
	content := config.DefaultDocument(version)
	cfg, err := config.Parse(content)
	if err != nil {
		return "", err
	}
	refs := []pluginref.Reference(cfg.Plugins)
	if err := project.Resolve(ctx, refs, nil); err != nil {
		return "", err
	}
	project.Manifest.original = content
	if err := project.Save(ctx, refs); err != nil {
		return "", err
	}
	return project.Manifest.Path, nil
}
