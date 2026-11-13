package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/input/discovery"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// ConfigurationMigration previews the syntax conversion and default plugin declarations.
// Applying it verifies dependencies before writing exact config and retiring an old lock.
type ConfigurationMigration struct {
	*config.FileMigration
	project       *Project
	refs          []pluginref.Reference
	updatePlugins bool
}

func PrepareConfigurationMigration(ctx context.Context, path string, options DefaultPluginOptions) (*ConfigurationMigration, error) {
	migration, err := options.RuleSet.PrepareMigration(ctx, path, options.Acquisition.Connections, options.Rules...)
	if errors.Is(err, config.ErrRuleMetadataRequired) {
		descriptors, metadataErr := cachedMigrationRules(ctx, options.RuleSet, options.Acquisition, path)
		if metadataErr != nil {
			return nil, fmt.Errorf("%w; inspect or install plugin metadata before migrating: %w", err, metadataErr)
		}
		migration, err = options.RuleSet.PrepareMigration(ctx, path, options.Acquisition.Connections, descriptors...)
	}
	if err != nil {
		return nil, err
	}
	project, err := OpenProject(ctx, options.RuleSet, options.Acquisition, filepath.Dir(migration.Path), "", migration.Path)
	if err != nil {
		return nil, err
	}
	if err := migration.CheckSources(project.Manifest.SourceContents()); err != nil {
		return nil, err
	}
	if err := migration.CheckUnchanged(ctx); err != nil {
		return nil, err
	}
	resolved, err := options.RuleSet.ResolveBeforePlugins(project.Manifest.MergedDocument(), options.Overlays)
	if err != nil {
		return nil, err
	}
	leaf, err := document.Parse(migration.Path, migration.Content)
	if err != nil {
		return nil, err
	}
	_, declaredLocally, err := configplugins.References(leaf.Value)
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
		refs, err = DefaultReferences(resolved.Config, options)
	}
	if err != nil {
		return nil, err
	}
	canonical, err := project.MigrationDeclarations(refs)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(refs, canonical) {
		updatePlugins = true
	}
	refs = canonical
	content := migration.Content
	if updatePlugins {
		content, err = configplugins.EditManifest(migration.Path, migration.Content, refs)
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
		notes, err := SourceFollowingNotes(refs)
		if err != nil {
			return nil, err
		}
		migration.Notes = append(migration.Notes, notes...)
	}
	project.StatusOptions.Warning = options.Warning
	return &ConfigurationMigration{FileMigration: migration, project: project, refs: refs, updatePlugins: updatePlugins}, nil
}

func (m *ConfigurationMigration) Apply(ctx context.Context) error {
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
		if err := m.project.RemoveLegacyLock(ctx); err != nil {
			return err
		}
		m.Changed = m.Changed || legacy
		return nil
	}
	resolved, err := m.project.ResolvedDeclarations(ctx, m.refs)
	if err != nil {
		return err
	}
	content, err := configplugins.EditManifest(m.Path, m.Content, resolved)
	if err != nil {
		return err
	}
	if err := m.SetContent(content); err != nil {
		return err
	}
	original := m.project.Manifest.OriginalBytes()
	m.project.Manifest.SetContent(m.Content)
	if err := m.project.Save(ctx, resolved); err != nil {
		m.project.Manifest.SetContent(original)
		return err
	}
	m.refs = resolved
	m.Changed = m.Changed || legacy
	return nil
}

// MigrateConfiguration automatically upgrades a selected legacy local file.
// Files already using current syntax are never implicitly enrolled in plugins.
func MigrateConfiguration(ctx context.Context, path string, options DefaultPluginOptions) error {
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

func prepareConfigMigration(ctx context.Context, path, repositoryRoot string, options DefaultPluginOptions) (*ConfigurationMigration, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Skip symlinked configs to avoid writing outside the selected project.
		return nil, nil
	}
	migration, err := options.RuleSet.PrepareMigration(ctx, path, options.Acquisition.Connections, options.Rules...)
	if errors.Is(err, config.ErrRuleMetadataRequired) {
		// Checking can bind legacy selections after plugin registration. Do
		// not force an implicit rewrite to inspect or execute plugin code.
		return nil, nil
	}
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
	prepared, err := PrepareConfigurationMigration(ctx, path, options)
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
func (s Selection) Migrate(ctx context.Context, directory string, input inputmodel.Request, options DefaultPluginOptions) error {
	if s.Disabled || len(s.Paths) != 0 || s.LockFile != "" {
		return nil
	}
	options.Overlays, options.Acquisition = s.Overlays, s.Acquisition
	if s.ConfigFile != "" {
		return MigrateConfiguration(ctx, s.ConfigFile, options)
	}
	starts := []string{directory}
	switch {
	case input.Kind == inputmodel.Files && len(input.Paths) != 0:
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, pluginSelectionStart(path))
		}
	case input.Kind == inputmodel.Stdin && input.Name != "" && input.Name != "<stdin>":
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
	case input.Kind == inputmodel.Stdin:
		return nil
	}
	seen := map[string]bool{}
	var candidates []struct {
		root string
		path string
	}
	for _, start := range starts {
		identity := discovery.PhysicalRepositoryRoot(start)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		root := discovery.RepositoryRoot(start)
		if root == "" {
			continue
		}
		path, err := configplugins.FindOrDefaultPath(filepath.Join(root, ".github"))
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
	plans := make([]*ConfigurationMigration, 0, len(candidates))
	for _, candidate := range candidates {
		plan, err := prepareConfigMigration(ctx, candidate.path, candidate.root, options)
		if err != nil {
			return err
		}
		if plan != nil {
			plans = append(plans, plan)
		}
	}
	if len(seen) > 1 && slices.ContainsFunc(plans, func(plan *ConfigurationMigration) bool {
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

// InitializeConfiguration verifies default plugin artifacts before creating the config
// and lockfile. A failed resolution leaves both project files untouched.
func InitializeConfiguration(ctx context.Context, rules config.RuleSet, acquisition Acquisition, root, version string, format config.FileFormat) (string, error) {
	lock, err := OpenSelectedLock(ctx, root, "", "")
	if err != nil {
		return "", err
	}
	path, err := configplugins.FindOrDefaultPath(filepath.Dir(lock.Path))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("config file already exists at %q", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	path = filepath.Join(filepath.Dir(path), format.Filename())
	project, err := OpenProject(ctx, rules, acquisition, root, "", path)
	if err != nil {
		return "", err
	}
	if project.Manifest.Exists {
		return "", fmt.Errorf("config file already exists at %q", project.Manifest.Path)
	}
	content, err := rules.DefaultDocument(version, format)
	if err != nil {
		return "", err
	}
	node, err := document.Parse(path, content)
	if err != nil {
		return "", err
	}
	defaults, _, err := configplugins.References(node.Value)
	if err != nil {
		return "", err
	}
	refs, err := project.Dependencies(true)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		refs = defaults
	}
	if err := project.Resolve(ctx, refs, nil); err != nil {
		return "", err
	}
	project.Manifest.SetContent(content)
	if err := project.Save(ctx, refs); err != nil {
		return "", err
	}
	return project.Manifest.Path, nil
}
