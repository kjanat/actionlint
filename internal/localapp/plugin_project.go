package localapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

// PluginProject keeps human-authored dependencies separate from resolved pins.
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
		name := pin.Name
		if pin.Alias != "" {
			name = pin.Alias
		}
		ref, err := pluginref.New(name, pin.Selector, pin.Registry)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func pluginPinMatches(pin pluginregistry.Pin, ref pluginref.Reference) bool {
	return pin.Matches(ref) && pin.Selector == ref.Version
}

// SelectedLock ignores stale extra pins: only declarations authorize loading.
func (p *PluginProject) SelectedLock() (pluginregistry.Lock, error) {
	refs, err := p.Dependencies(false)
	if err != nil {
		return pluginregistry.Lock{}, err
	}
	selected := pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{}}
	for _, ref := range refs {
		if ref.DirectSource() != "" {
			continue
		}
		found := false
		for _, pin := range p.Lock.Lock.Plugins {
			if pluginPinMatches(pin, ref) {
				selected.Plugins = append(selected.Plugins, pin)
				found = true
				break
			}
		}
		if !found {
			return pluginregistry.Lock{}, fmt.Errorf("plugin %s is missing or out of date in %s; run actionlint plugin install", ref.String(), p.Lock.Path)
		}
	}
	return selected, selected.Validate()
}

// Resolve acquires missing dependencies and refreshes the specified declarations.
// Existing immutable identity checks still apply to every replacement.
func (p *PluginProject) Resolve(ctx context.Context, refs, update []pluginref.Reference) error {
	p.Lock.Lock.Plugins = slices.DeleteFunc(p.Lock.Lock.Plugins, func(pin pluginregistry.Pin) bool {
		return !slices.ContainsFunc(refs, func(ref pluginref.Reference) bool {
			return pin.Matches(ref) || !ref.IsAlias() && strings.EqualFold(pin.Name, ref.Name)
		})
	})
	selected := make([]pluginregistry.Pin, 0, len(refs))
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			if _, err := artifact.Resolve(ctx, source, artifactOptions()); err != nil {
				return err
			}
			continue
		}
		var pin pluginregistry.Pin
		found := false
		for _, old := range p.Lock.Lock.Plugins {
			if pluginPinMatches(old, ref) {
				pin, found = old, true
				break
			}
		}
		if !found || slices.Contains(update, ref) {
			client, err := pluginregistry.New(ref.Registry)
			if err != nil {
				return err
			}
			client.StatusOptions = p.StatusOptions
			pin, err = client.Resolve(ctx, ref.Name, ref.Version)
			if err != nil {
				return err
			}
			if err := p.Lock.SetResolved(pin, ref); err != nil {
				return err
			}
		}
		selected = append(selected, pin)
	}
	p.Lock.Lock.Plugins = selected
	if err := p.Lock.Lock.Validate(); err != nil {
		return err
	}
	if err := p.Lock.Lock.CheckStatuses(ctx, p.StatusOptions); err != nil {
		return err
	}
	return SyncPlugins(ctx, p.Lock.Lock)
}

// Save serializes cooperating writers. The lock is replaced first; a failed
// manifest write restores it only if nobody has changed it in the meantime.
// SelectedLock rejects missing or mismatched pins after an interrupted save.
func (p *PluginProject) Save(ctx context.Context, refs []pluginref.Reference) (err error) {
	if err := p.Lock.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Lock.Path), 0o755); err != nil {
		return err
	}
	guard := p.Lock.Path + ".project-writing"
	if err := os.Mkdir(guard, 0o700); err != nil {
		return fmt.Errorf("plugin project is busy: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(guard)) }()
	if err := p.Manifest.CheckUnchanged(ctx); err != nil {
		return err
	}
	writeLock := p.Lock.Exists() || len(p.Lock.Lock.Plugins) != 0
	if writeLock {
		if err := p.Lock.Save(ctx); err != nil {
			return err
		}
	}
	if err := p.Manifest.Save(ctx, refs); err != nil {
		if !writeLock {
			return err
		}
		return errors.Join(err, p.Lock.Restore(context.WithoutCancel(ctx), &p.original))
	}
	p.original = *p.Lock
	p.original.Lock.Plugins = slices.Clone(p.Lock.Lock.Plugins)
	return nil
}
