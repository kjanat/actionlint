package localapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/projectpath"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
)

// DefaultPluginOptions preserves invocation-level native selections during migration.
// Configured backends and explicit configuration disables take precedence.
type DefaultPluginOptions struct {
	Shellcheck bool
	Ruff       bool
	Warning    func(string)
	Overlays   []config.Overlay
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
	project       *PluginProject
	refs          []pluginref.Reference
	updatePlugins bool
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
	resolved, err := config.ResolveBeforePlugins(project.Manifest.node, options.Overlays)
	if err != nil {
		return nil, err
	}
	leaf, err := configtree.Decode(migration.Path, migration.Content)
	if err != nil {
		return nil, err
	}
	_, declaredLocally, err := manifestReferences(leaf)
	if err != nil {
		return nil, err
	}
	updatePlugins := true
	var refs []pluginref.Reference
	switch {
	case !declaredLocally && project.Manifest.Declared:
		// Keep inherited declarations inherited. Writing the effective list into
		// the leaf would freeze future changes made by the shared base.
		refs = slices.Clone(project.Manifest.References)
		updatePlugins = false
	case !declaredLocally && len(project.Lock.Lock.Plugins) != 0:
		if resolved.Config.Plugins != nil {
			// An invocation overlay explicitly selected plugins for this migration.
			refs = slices.Clone(resolved.Config.Plugins)
		} else {
			// A lock-only project predates manifest declarations. Migrate its pins
			// into the manifest while plugins remain unspecified.
			refs, err = project.Dependencies(true)
		}
	default:
		refs, err = DefaultPluginReferences(resolved.Config, options)
	}
	if err != nil {
		return nil, err
	}
	content := migration.Content
	if updatePlugins {
		content, err = editPluginManifest(migration.Path, migration.Content, refs)
		if err != nil {
			return nil, err
		}
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
	return &ConfigMigration{FileMigration: migration, project: project, refs: refs, updatePlugins: updatePlugins}, nil
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
	if !m.updatePlugins {
		return m.FileMigration.Apply()
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
	return migrateConfig(ctx, path, "", options)
}

func migrateConfig(ctx context.Context, path, repositoryRoot string, options DefaultPluginOptions) error {
	prepared, err := prepareConfigMigration(ctx, path, repositoryRoot, options)
	if err != nil || prepared == nil {
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

func prepareConfigMigration(ctx context.Context, path, repositoryRoot string, options DefaultPluginOptions) (*ConfigMigration, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Reading a linked config remains supported; automatic writes never
		// change its target outside the selected project.
		return nil, nil
	}
	migration, err := config.PrepareMigration(path)
	if err != nil || !migration.Changed {
		return nil, err
	}
	if repositoryRoot != "" {
		inside, err := migrationPathInsideRepository(repositoryRoot, migration.Path)
		if err != nil {
			return nil, err
		}
		if !inside {
			return nil, nil
		}
	}
	prepared, err := PrepareConfigMigration(ctx, path, options)
	if err != nil {
		return nil, err
	}
	if repositoryRoot != "" {
		inside, err := migrationPathInsideRepository(repositoryRoot, prepared.Path)
		if err != nil {
			return nil, err
		}
		if !inside {
			return nil, nil
		}
	}
	return prepared, nil
}

func migrationPathInsideRepository(root, path string) (bool, error) {
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	physicalParent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return false, err
	}
	physicalPath := filepath.Join(physicalParent, filepath.Base(absolute))
	relative, err := filepath.Rel(physicalRoot, physicalPath)
	if err != nil {
		return false, fmt.Errorf("cannot compare migration path with repository root: %w", err)
	}
	return filepath.IsLocal(relative), nil
}

// Migrate upgrades selected local configs before runtime plugin selection.
// Cache planning calls SelectForCache and remains read-only.
func (s PluginSelection) Migrate(ctx context.Context, directory string, input application.Input, options DefaultPluginOptions) error {
	if s.Disabled || len(s.Paths) != 0 || s.LockFile != "" {
		return nil
	}
	options.Overlays = s.Overlays
	if s.ConfigFile != "" {
		return MigrateConfig(ctx, s.ConfigFile, options)
	}
	starts := []string{directory}
	switch {
	case input.Kind == application.Files && len(input.Paths) != 0:
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, filepath.Dir(path))
		}
	case input.Kind == application.Stdin && input.Name != "" && input.Name != "<stdin>":
		path := input.Name
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		starts = []string{filepath.Dir(path)}
	case input.Kind == application.Stdin:
		return nil
	}
	seen := map[string]bool{}
	var candidates []struct {
		root string
		path string
	}
	for _, start := range starts {
		identity := pluginProjectRoot(start)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		root := projectpath.RepositoryRoot(start)
		if root == "" {
			continue
		}
		path, err := pluginConfigPath(filepath.Join(root, ".github"))
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		candidates = append(candidates, struct {
			root string
			path string
		}{root, path})
	}
	if len(seen) > 1 {
		// Check existing plugin scopes before planning migrations. Selection is
		// read-only and catches active configs in repositories with no legacy file.
		if _, err := s.SelectForCache(ctx, directory, input); err != nil {
			return err
		}
	}
	plans := make([]*ConfigMigration, 0, len(candidates))
	for _, candidate := range candidates {
		plan, err := prepareConfigMigration(ctx, candidate.path, candidate.root, options)
		if err != nil {
			return err
		}
		if plan != nil {
			plans = append(plans, plan)
		}
	}
	if len(seen) > 1 && slices.ContainsFunc(plans, func(plan *ConfigMigration) bool {
		return len(plan.refs) != 0
	}) {
		return errors.New("inputs span repositories with plugin configuration; check them separately or select --plugin-lock explicitly")
	}
	for _, plan := range plans {
		if err := plan.Apply(ctx); err != nil {
			return err
		}
		if options.Warning != nil {
			options.Warning("migrated configuration: " + plan.Path)
			for _, note := range plan.Notes {
				options.Warning(note)
			}
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
	refs, err := project.Dependencies(true)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		refs = []pluginref.Reference(cfg.Plugins)
	}
	if err := project.Resolve(ctx, refs, nil); err != nil {
		return "", err
	}
	project.Manifest.original = content
	if err := project.Save(ctx, refs); err != nil {
		return "", err
	}
	return project.Manifest.Path, nil
}
