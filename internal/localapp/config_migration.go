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
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

// DefaultPluginOptions preserves invocation-level native selections during migration.
// Configured backends and explicit configuration disables take precedence.
type DefaultPluginOptions struct {
	// Tools lists the bundled tool ids that run their default command in
	// this invocation. Each may receive a default plugin declaration; ids
	// the invocation selected or disabled explicitly are left out.
	Tools    []string
	Warning  func(string)
	Overlays []config.Overlay
}

// DefaultPluginReferences supplies the v2 defaults only when plugins was omitted.
// An explicit empty list is a durable opt-out, including through inheritance.
// It declares, in bundled directory order, the registry plugin named after
// each bundled tool in options.Tools that configuration leaves enabled and
// on its default backend.
func DefaultPluginReferences(cfg *config.Config, options DefaultPluginOptions) ([]pluginref.Reference, error) {
	if cfg.Plugins != nil {
		return slices.Clone(cfg.Plugins), nil
	}
	refs := []pluginref.Reference{}
	for _, id := range registry.IDs() {
		tool := cfg.Tools[id]
		if !slices.Contains(options.Tools, id) || tool.Backend != nil || tool.Enabled != nil && !*tool.Enabled || config.DiagnosticLevel(cfg, id) == "off" {
			continue
		}
		ref, err := pluginref.Parse(id)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// sourceFollowingNotes warns, for each default registry plugin in refs that
// replaces a bundled tool reporting findings in files its scripts source,
// that the isolated plugin stops following those files.
func sourceFollowingNotes(refs []pluginref.Reference) ([]string, error) {
	manifests, err := provider.Bundled()
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		if !manifest.Capabilities.ReportedSources || !slices.ContainsFunc(refs, func(ref pluginref.Reference) bool {
			return ref.Name() == id && ref.Registry() == pluginref.DefaultRegistry
		}) {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s now uses the isolated Wasm plugin, which does not follow files the script sources. Explicit native configuration in tools.%s keeps the native backend.", id, id))
	}
	return notes, nil
}

// ConfigMigration previews the syntax conversion and default plugin declarations.
// Applying it verifies dependencies before writing exact config and retiring an old lock.
type ConfigMigration struct {
	*config.FileMigration
	project       *PluginProject
	refs          []pluginref.Reference
	updatePlugins bool
}

func PrepareConfigMigration(ctx context.Context, path string, options DefaultPluginOptions) (*ConfigMigration, error) {
	migration, err := config.PrepareMigration(ctx, path)
	if err != nil {
		return nil, err
	}
	project, err := OpenPluginProject(ctx, filepath.Dir(migration.Path), "", migration.Path)
	if err != nil {
		return nil, err
	}
	if err := migration.CheckSources(project.Manifest.snapshots); err != nil {
		return nil, err
	}
	if err := migration.CheckUnchanged(ctx); err != nil {
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
			// Import lockfile pins for projects without a plugin manifest.
			refs, err = project.Dependencies(true)
		}
	default:
		refs, err = DefaultPluginReferences(resolved.Config, options)
	}
	if err != nil {
		return nil, err
	}
	canonical, err := project.migrationDeclarations(refs)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(refs, canonical) {
		updatePlugins = true
	}
	refs = canonical
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
	if project.Lock.Exists() {
		migration.Changed = true
		migration.Notes = append(migration.Notes, "Migrate exact plugin versions into configuration and remove the legacy project lock after verification.")
	}
	if !project.Manifest.Declared {
		notes, err := sourceFollowingNotes(refs)
		if err != nil {
			return nil, err
		}
		migration.Notes = append(migration.Notes, notes...)
	}
	project.StatusOptions.Warning = options.Warning
	return &ConfigMigration{FileMigration: migration, project: project, refs: refs, updatePlugins: updatePlugins}, nil
}

func (m *ConfigMigration) Apply(ctx context.Context) error {
	legacy := m.project.Lock.Exists()
	if err := m.SetContent(m.Content); err != nil {
		return err
	}
	if !m.Changed && !legacy {
		return nil
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if !m.updatePlugins && !legacy {
		return m.FileMigration.Apply(ctx)
	}
	if err := m.project.Resolve(ctx, m.refs, nil); err != nil {
		return fmt.Errorf("config migration left %s unchanged: %w", m.Path, err)
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if !m.updatePlugins {
		if err := m.FileMigration.Apply(ctx); err != nil {
			return err
		}
		if err := m.project.removeLegacyLock(ctx); err != nil {
			return err
		}
		m.Changed = m.Changed || legacy
		return nil
	}
	resolved, err := m.project.resolvedDeclarations(ctx, m.refs)
	if err != nil {
		return err
	}
	content, err := editPluginManifest(m.Path, m.Content, resolved)
	if err != nil {
		return err
	}
	if err := m.SetContent(content); err != nil {
		return err
	}
	original := m.project.Manifest.original
	m.project.Manifest.original = bytes.Clone(m.Content)
	if err := m.project.Save(ctx, resolved); err != nil {
		m.project.Manifest.original = original
		return err
	}
	m.refs = resolved
	m.Changed = m.Changed || legacy
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
		// Skip symlinked configs to avoid writing outside the selected project.
		return nil, nil
	}
	migration, err := config.PrepareMigration(ctx, path)
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
