package localapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact/oci"
)

// PluginProject edits exact declarations and reads old project locks for migration.
// Resolved registry metadata is stored only in the user cache.
type PluginProject struct {
	Manifest      *PluginManifest
	Lock          *pluginregistry.LockFile
	StatusOptions pluginregistry.StatusOptions
	original      pluginregistry.LockFile
}

// DirectSources lists artifacts whose URL and digest are pinned in configuration.
func (p *PluginProject) DirectSources() []string {
	var sources []string
	for _, ref := range p.Manifest.References {
		if source := ref.DirectSource(); source != "" {
			sources = append(sources, source)
		}
	}
	return sources
}

// SyncDirect restores directly declared artifacts without registry resolution.
func (p *PluginProject) SyncDirect(ctx context.Context) error {
	for _, source := range p.DirectSources() {
		if _, err := artifact.Resolve(ctx, source, artifactOptions()); err != nil {
			return err
		}
		if artifact.IsOCI(source) {
			if err := oci.PullLayers(ctx, source, artifactOptions()); err != nil {
				return err
			}
		}
	}
	return nil
}

func pluginConfigPath(directory string) (string, error) {
	for _, name := range repoconfig.RepositoryConfigNames {
		path := filepath.Join(directory, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return filepath.Join(directory, "actionlint.yaml"), nil
}

func OpenPluginProject(ctx context.Context, directory, lockPath, configPath string) (*PluginProject, error) {
	lock, err := OpenPluginLock(ctx, directory, lockPath, configPath)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath, err = pluginConfigPath(filepath.Dir(lock.Path))
		if err != nil {
			return nil, err
		}
	}
	manifest, err := OpenPluginManifest(ctx, configPath)
	if err != nil {
		return nil, err
	}
	original := *lock
	original.Lock.Plugins = slices.Clone(lock.Lock.Plugins)
	return &PluginProject{Manifest: manifest, Lock: lock, original: original}, nil
}

// Dependencies migrates lock-only installations only during an explicit mutation.
// Read-only checking never lets an undeclared lock entry activate a plugin.
func (p *PluginProject) Dependencies(migrate bool) ([]pluginref.Reference, error) {
	if p.Manifest.Declared {
		return slices.Clone(p.Manifest.References), nil
	}
	if len(p.Lock.Lock.Plugins) != 0 && !migrate {
		return nil, fmt.Errorf("plugins are locked but not declared in %s; run actionlint plugin install to migrate", p.Manifest.Path)
	}
	refs := make([]pluginref.Reference, 0, len(p.Lock.Lock.Plugins))
	for _, pin := range p.Lock.Lock.Plugins {
		ref, err := pluginref.New(pin.Name, pin.Version, pin.Registry)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// SelectedPlugins resolves only configured exact releases; old lock entries never
// activate undeclared plugins. It never writes project files.
func (p *PluginProject) SelectedPlugins(ctx context.Context) (pluginregistry.Lock, error) {
	refs, err := p.Dependencies(false)
	if err != nil {
		return pluginregistry.Lock{}, err
	}
	return p.selectedPins(ctx, refs)
}

func (p *PluginProject) selectedPins(ctx context.Context, refs []pluginref.Reference) (pluginregistry.Lock, error) {
	selected := pluginregistry.Lock{SchemaVersion: pluginregistry.LockSchemaVersion, Plugins: []pluginregistry.Pin{}}
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			// A direct oci:// source runs without its lock entry, which
			// only lists its blobs ahead of the pull.
			if pin, ok := p.Lock.Lock.SourcePin(source); ok {
				selected.Sources = append(selected.Sources, pin)
			}
			continue
		}
		if p.Lock.Exists() {
			// Existing projects may still declare an alias/latest alongside their
			// old exact lock. Honor those bytes read-only until explicit migration.
			var legacy *pluginregistry.Pin
			for _, old := range p.Lock.Lock.Plugins {
				if old.Matches(ref) && (ref.Version() == old.Selector || ref.Version() == old.Version) {
					legacy = new(old)
					break
				}
			}
			if legacy != nil {
				selected.Plugins = append(selected.Plugins, *legacy)
				continue
			}
		}
		client, err := pluginregistry.New(ref.Registry())
		if err != nil {
			return pluginregistry.Lock{}, err
		}
		client.StatusOptions = p.StatusOptions
		pin, err := client.Lookup(ctx, ref.Name(), ref.Version())
		if err != nil {
			return pluginregistry.Lock{}, err
		}
		selected.Plugins = append(selected.Plugins, pin)
	}
	return selected, selected.Validate()
}

// Resolve acquires missing dependencies and refreshes the specified declarations.
// Existing immutable identity checks still apply to every replacement.
func (p *PluginProject) Resolve(ctx context.Context, refs, update []pluginref.Reference) error {
	selected := make([]pluginregistry.Pin, 0, len(refs))
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			pulled, err := artifact.Resolve(ctx, source, artifactOptions())
			if err != nil {
				return err
			}
			if pulled.Info.Kind == artifact.ProviderManifest {
				// Refuse a broken provider now rather than at check time.
				if _, err := pulledProvider(pulled); err != nil {
					return err
				}
			}
			if artifact.IsOCI(source) {
				if err := oci.PullLayers(ctx, source, artifactOptions()); err != nil {
					return err
				}
			}
			continue
		}
		client, err := pluginregistry.New(ref.Registry())
		if err != nil {
			return err
		}
		client.StatusOptions = p.StatusOptions
		var pin pluginregistry.Pin
		var previous *pluginregistry.Pin
		for _, old := range p.original.Lock.Plugins {
			if old.Matches(ref) && (ref.Version() == old.Version || ref.Version() == old.Selector) {
				previous = new(old)
				break
			}
		}
		switch {
		case previous != nil && !slices.Contains(update, ref):
			// Migration freezes the recorded release, including old latest selectors.
			pin, err = client.Lookup(ctx, previous.Name, previous.Version)
			if err == nil && !pin.SameRelease(*previous) {
				return fmt.Errorf("registry release disagrees with legacy lock for %s@%s", previous.Name, previous.Version)
			}
		case ref.Version() != "latest" && !ref.IsAlias() && !slices.Contains(update, ref):
			pin, err = client.Lookup(ctx, ref.Name(), ref.Version())
		default:
			pin, err = client.Resolve(ctx, ref.Name(), ref.Version())
		}
		if err != nil {
			return err
		}
		pin.Selector = ref.Version()
		if ref.IsAlias() {
			pin.Alias = ref.Name()
		}
		if err := p.original.CheckResolved(pin, ref); err != nil {
			return err
		}
		if err := p.checkConfiguredRelease(ctx, pin, ref); err != nil {
			return err
		}
		selected = append(selected, pin)
	}
	p.Lock.Lock.Plugins, p.Lock.Lock.Sources = selected, nil
	if err := p.Lock.Lock.Validate(); err != nil {
		return err
	}
	if err := p.Lock.Lock.CheckStatuses(ctx, p.StatusOptions); err != nil {
		return err
	}
	return SyncPlugins(ctx, p.Lock.Lock)
}

// Configured releases retain publisher, immutable-version and downgrade checks
// after project locks are retired. Their authority evidence comes from the cache
// or the same registry lookup used by normal exact declarations.
func (p *PluginProject) checkConfiguredRelease(ctx context.Context, pin pluginregistry.Pin, ref pluginref.Reference) error {
	for _, configured := range p.Manifest.References {
		if configured.Name() != pin.Name || configured.IsAlias() || configured.Version() == "latest" {
			continue
		}
		client, err := pluginregistry.New(configured.Registry())
		if err != nil {
			return err
		}
		client.StatusOptions = p.StatusOptions
		previous, err := client.Lookup(ctx, configured.Name(), configured.Version())
		if err != nil {
			return err
		}
		baseline := pluginregistry.LockFile{Lock: pluginregistry.Lock{Plugins: []pluginregistry.Pin{previous}}}
		if err := baseline.CheckResolved(pin, ref); err != nil {
			return err
		}
	}
	return nil
}

// resolvedDeclarations collapses selection intent to exact canonical releases.
// Publisher identity and checksums remain registry/cache evidence.
func (p *PluginProject) resolvedDeclarations(ctx context.Context, refs []pluginref.Reference) ([]pluginref.Reference, error) {
	resolved := make([]pluginref.Reference, 0, len(refs))
	for _, ref := range refs {
		if ref.DirectSource() != "" {
			resolved = append(resolved, ref)
			continue
		}
		var selected *pluginregistry.Pin
		for _, pin := range p.Lock.Lock.Plugins {
			if pin.Matches(ref) && (ref.Version() == "latest" || ref.Version() == pin.Version || ref.Version() == pin.Selector) {
				selected = new(pin)
				break
			}
		}
		if selected == nil {
			if ref.IsAlias() || ref.Version() == "latest" {
				return nil, fmt.Errorf("resolve plugin %s before saving its declaration", ref.String())
			}
			resolved = append(resolved, ref)
			continue
		}
		client, err := pluginregistry.New(selected.Registry)
		if err != nil {
			return nil, err
		}
		client.StatusOptions = p.StatusOptions
		verified, err := client.Lookup(ctx, selected.Name, selected.Version)
		if err != nil {
			return nil, err
		}
		if !verified.SameRelease(*selected) {
			return nil, fmt.Errorf("registry release disagrees with selected artifact for %s@%s", selected.Name, selected.Version)
		}
		declaration, err := pluginref.New(selected.Name, selected.Version, selected.Registry)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, declaration)
	}
	return resolved, nil
}

// Save writes one exact plugin declaration list. An old lock is removed only
// after config is safely written and the lock still matches the input snapshot.
func (p *PluginProject) Save(ctx context.Context, refs []pluginref.Reference) (err error) {
	resolved, err := p.resolvedDeclarations(ctx, refs)
	if err != nil {
		return err
	}
	if err := p.Lock.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := p.Manifest.Save(ctx, resolved); err != nil {
		return err
	}
	return p.removeLegacyLock(ctx)
}

func (p *PluginProject) removeLegacyLock(ctx context.Context) error {
	if !p.Lock.Exists() {
		return nil
	}
	if err := p.Lock.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.Remove(p.Lock.Path); err != nil {
		return fmt.Errorf("configuration migrated but legacy plugin lock could not be removed: %w", err)
	}
	lock, err := pluginregistry.OpenLock(ctx, p.Lock.Path)
	if err == nil {
		lock.Lock = p.Lock.Lock
		p.Lock = lock
		p.original = *lock
	}
	return err
}

// migrationDeclarations uses old pins only to preserve the release already
// selected by a matching declaration. Apply verifies these against the registry.
func (p *PluginProject) migrationDeclarations(refs []pluginref.Reference) ([]pluginref.Reference, error) {
	resolved := slices.Clone(refs)
	for index, ref := range refs {
		if ref.DirectSource() != "" {
			continue
		}
		for _, pin := range p.original.Lock.Plugins {
			if !pin.Matches(ref) {
				continue
			}
			if ref.Version() != pin.Version && ref.Version() != pin.Selector {
				return nil, fmt.Errorf("plugin %s disagrees with legacy locked version %s; use plugin install to select a replacement", ref.String(), pin.Version)
			}
			declaration, err := pluginref.New(pin.Name, pin.Version, pin.Registry)
			if err != nil {
				return nil, err
			}
			resolved[index] = declaration
			break
		}
	}
	return resolved, nil
}
