package installation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// Project edits exact declarations and reads old project locks for migration.
// Resolved registry metadata is stored only in the user cache.
type Project struct {
	Manifest       *configplugins.File
	Lock           *LockFile
	StatusOptions  StatusOptions
	original       LockFile
	defaultPlugins bool
}

// DirectSources lists artifacts whose URL and digest are pinned in configuration.
func (p *Project) DirectSources() []string {
	var sources []string
	for _, ref := range p.Manifest.References {
		if source := ref.DirectSource(); source != "" {
			sources = append(sources, source)
		}
	}
	return sources
}

// SyncDirect restores directly declared artifacts without registry resolution.
func (p *Project) SyncDirect(ctx context.Context) error {
	for _, source := range p.DirectSources() {
		if _, err := artifacts.Resolve(ctx, source, p.artifactOptions()); err != nil {
			return err
		}
		if artifacts.IsOCI(source) {
			if err := oci.PullLayers(ctx, source, p.artifactOptions()); err != nil {
				return err
			}
		}
	}
	return nil
}

func OpenProject(ctx context.Context, rules config.RuleSet, acquisition Acquisition, directory, lockPath, configPath string) (*Project, error) {
	lock, err := OpenSelectedLock(ctx, directory, lockPath, configPath)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath, err = configplugins.FindOrDefaultPath(filepath.Dir(lock.Path))
		if err != nil {
			return nil, err
		}
	}
	manifest, err := configplugins.Open(ctx, rules, configPath, acquisition.Connections)
	if err != nil {
		return nil, err
	}
	original := *lock
	original.Lock.Plugins = slices.Clone(lock.Lock.Plugins)
	return &Project{Manifest: manifest, Lock: lock, StatusOptions: StatusOptions{Acquisition: acquisition}, original: original}, nil
}

// Dependencies migrates lock-only installations only during an explicit mutation.
// Read-only checking never lets an undeclared lock entry activate a plugin.
func (p *Project) Dependencies(migrate bool) ([]pluginref.Reference, error) {
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
func (p *Project) SelectedPlugins(ctx context.Context) (Lock, error) {
	refs, err := p.Dependencies(false)
	if err != nil {
		return Lock{}, err
	}
	return p.selectedPins(ctx, refs)
}

func (p *Project) selectedPins(ctx context.Context, refs []pluginref.Reference) (Lock, error) {
	selected := Lock{SchemaVersion: LockSchemaVersion, Plugins: []Pin{}}
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			// A direct oci:// source runs without its lock entry, which
			// only lists its blobs ahead of the pull.
			if pin, ok := p.Lock.Lock.SourcePin(source); ok {
				selected.Sources = append(selected.Sources, pin)
			}
			continue
		}
		if digest := ref.CandidateDigest(); digest != "" {
			client, err := NewWithGrant(ref.Registry(), p.StatusOptions)
			if err != nil {
				return Lock{}, err
			}
			pin, err := client.LookupCandidate(ctx, ref.Name(), ref.Version(), digest)
			if err != nil {
				return Lock{}, err
			}
			selected.Plugins = append(selected.Plugins, pin)
			continue
		}
		if p.Lock.Exists() {
			// Existing projects may still declare an alias/latest alongside their
			// old exact lock. Honor those bytes read-only until explicit migration.
			var legacy *Pin
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
		client, err := NewWithGrant(ref.Registry(), p.StatusOptions)
		if err != nil {
			return Lock{}, err
		}
		var pin Pin
		if p.defaultPlugins {
			pin, err = client.ResolveCached(ctx, ref.Name(), ref.Version())
		} else {
			pin, err = client.Lookup(ctx, ref.Name(), ref.Version())
		}
		if err != nil {
			return Lock{}, err
		}
		selected.Plugins = append(selected.Plugins, pin)
	}
	return selected, selected.Validate()
}

// Resolve acquires missing dependencies and refreshes the specified declarations.
// Existing immutable identity checks still apply to every replacement.
func (p *Project) Resolve(ctx context.Context, refs, update []pluginref.Reference) error {
	selected := make([]Pin, 0, len(refs))
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			if manifests.IsLocalManifest(source) {
				if err := ValidateLocalProvider(ctx, source); err != nil {
					return err
				}
				continue
			}
			options := p.artifactOptions()
			pulled, err := artifacts.Resolve(ctx, source, options)
			if err != nil {
				return err
			}
			if pulled.Info.Kind == artifacts.ProviderManifest {
				// Refuse an unusable provider before saving its declaration.
				if err := ValidateProviderArtifact(ctx, pulled, options); err != nil {
					return err
				}
			}
			if artifacts.IsOCI(source) {
				if err := oci.PullLayers(ctx, source, p.artifactOptions()); err != nil {
					return err
				}
			}
			continue
		}
		client, err := NewWithGrant(ref.Registry(), p.StatusOptions)
		if err != nil {
			return err
		}
		var pin Pin
		var previous *Pin
		for _, old := range p.original.Lock.Plugins {
			if old.Matches(ref) && (ref.Version() == old.Version || ref.Version() == old.Selector) {
				previous = new(old)
				break
			}
		}
		candidate := strings.HasPrefix(ref.Registry(), "file:")
		updating := slices.Contains(update, ref)
		switch {
		case candidate && !updating && ref.CandidateDigest() != "":
			pin, err = client.LookupCandidate(ctx, ref.Name(), ref.Version(), ref.CandidateDigest())
		case candidate && !updating && previous != nil && previous.Origin == "candidate":
			pin, err = client.LookupCandidate(ctx, previous.Name, previous.Version, previous.SHA256)
		case candidate:
			// Explicit install resolves the current staging bytes. Only update can
			// replace an existing bound digest.
			pin, err = client.Resolve(ctx, ref.Name(), ref.Version())
		case previous != nil && !updating:
			// Migration freezes the recorded release, including old latest selectors.
			pin, err = client.Lookup(ctx, previous.Name, previous.Version)
			if err == nil && !pin.SameRelease(*previous) {
				return fmt.Errorf("registry release disagrees with legacy lock for %s@%s", previous.Name, previous.Version)
			}
		case ref.Version() != "latest" && !ref.IsAlias() && !updating:
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
		if err := p.Lock.CheckResolved(pin, ref); err != nil {
			return err
		}
		if err := p.checkConfiguredRelease(ctx, pin, ref); err != nil {
			return err
		}
		selected = append(selected, pin)
	}
	next := p.Lock.Lock
	next.Plugins, next.Sources = selected, nil
	if err := next.Validate(); err != nil {
		return err
	}
	if err := next.CheckStatuses(ctx, p.StatusOptions); err != nil {
		return err
	}
	if err := Sync(ctx, next, p.StatusOptions.Acquisition); err != nil {
		return err
	}
	for _, pin := range next.Plugins {
		if pin.Kind != artifacts.ProviderManifest {
			continue
		}
		options := p.artifactOptions()
		pulled, err := artifacts.Resolve(ctx, pin.Source(), options)
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		if pulled.Info.Kind != artifacts.ProviderManifest {
			return fmt.Errorf("plugin %s@%s: expected a process provider manifest, found %s", pin.Name, pin.Version, pulled.Info.Kind)
		}
		if err := ValidateProviderArtifact(ctx, pulled, options); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
	}
	for _, pin := range next.Plugins {
		if pin.Origin != "candidate" {
			continue
		}
		client, err := NewWithGrant(pin.Registry, p.StatusOptions)
		if err != nil {
			return err
		}
		options := p.artifactOptions()
		options.PullOCI = oci.Cached
		if err := client.RememberCandidate(ctx, pin, options); err != nil {
			return err
		}
	}
	p.Lock.Lock = next
	return nil
}

// Configured releases retain publisher, immutable-version and downgrade checks
// after project locks are retired. Their authority evidence comes from the cache
// or the same registry lookup used by normal exact declarations.
func (p *Project) checkConfiguredRelease(ctx context.Context, pin Pin, ref pluginref.Reference) error {
	for _, configured := range p.Manifest.References {
		if configured.Name() != pin.Name || configured.IsAlias() || configured.Version() == "latest" {
			continue
		}
		client, err := NewWithGrant(configured.Registry(), p.StatusOptions)
		if err != nil {
			return err
		}
		var previous Pin
		switch {
		case configured.CandidateDigest() != "":
			previous, err = client.LookupCandidate(ctx, configured.Name(), configured.Version(), configured.CandidateDigest())
		case strings.HasPrefix(configured.Registry(), "file:"):
			// A new candidate declaration has no previous release evidence to
			// compare. Its bytes are verified during installation and then pinned.
			continue
		default:
			previous, err = client.Lookup(ctx, configured.Name(), configured.Version())
		}
		if err != nil {
			return err
		}
		baseline := LockFile{Lock: Lock{Plugins: []Pin{previous}}}
		if err := baseline.CheckResolved(pin, ref); err != nil {
			return err
		}
	}
	return nil
}

// ResolvedDeclarations replaces selectors with exact release versions.
// Registry and cache records supply publisher identities and checksums.
func (p *Project) ResolvedDeclarations(ctx context.Context, refs []pluginref.Reference) ([]pluginref.Reference, error) {
	resolved := make([]pluginref.Reference, 0, len(refs))
	for _, ref := range refs {
		if ref.DirectSource() != "" {
			resolved = append(resolved, ref)
			continue
		}
		var selected *Pin
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
		client, err := NewWithGrant(selected.Registry, p.StatusOptions)
		if err != nil {
			return nil, err
		}
		var verified Pin
		if selected.Origin == "candidate" {
			verified, err = client.LookupCandidate(ctx, selected.Name, selected.Version, selected.SHA256)
		} else {
			verified, err = client.Lookup(ctx, selected.Name, selected.Version)
		}
		if err != nil {
			return nil, err
		}
		if !verified.SameRelease(*selected) {
			return nil, fmt.Errorf("registry release disagrees with selected artifact for %s@%s", selected.Name, selected.Version)
		}
		var declaration pluginref.Reference
		if selected.Origin == "candidate" {
			declaration, err = pluginref.NewCandidate(selected.Name, selected.Version, selected.Registry, selected.SHA256)
		} else {
			declaration, err = pluginref.New(selected.Name, selected.Version, selected.Registry)
		}
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, declaration)
	}
	return resolved, nil
}

// Save writes one exact plugin declaration list. An old lock is removed only
// after config is safely written and the lock still matches the input snapshot.
func (p *Project) Save(ctx context.Context, refs []pluginref.Reference) (err error) {
	resolved, err := p.ResolvedDeclarations(ctx, refs)
	if err != nil {
		return err
	}
	if err := p.Lock.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := p.Manifest.Save(ctx, resolved); err != nil {
		return err
	}
	return p.RemoveLegacyLock(ctx)
}

func (p *Project) RemoveLegacyLock(ctx context.Context) error {
	if !p.Lock.Exists() {
		return nil
	}
	if err := p.Lock.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.Remove(p.Lock.Path); err != nil {
		return fmt.Errorf("configuration migrated but legacy plugin lock could not be removed: %w", err)
	}
	lock, err := OpenLock(ctx, p.Lock.Path)
	if err == nil {
		lock.Lock = p.Lock.Lock
		p.Lock = lock
		p.original = *lock
	}
	return err
}

// MigrationDeclarations uses old pins only to preserve the release already
// selected by a matching declaration. Apply verifies these against the registry.
func (p *Project) MigrationDeclarations(refs []pluginref.Reference) ([]pluginref.Reference, error) {
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

func (p *Project) artifactOptions() artifacts.Options {
	options := p.StatusOptions.Acquisition.ArtifactOptions()
	options.Network = p.StatusOptions.Network
	return options
}
