package installation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// Project edits exact plugin declarations in configuration.
// Resolved registry metadata is stored only in the user cache.
type Project struct {
	Manifest       *configplugins.File
	Releases       ResolvedPlugins
	StatusOptions  StatusOptions
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

func projectConfigPath(directory, selected string) (string, error) {
	if selected != "" {
		return selected, nil
	}
	repository := discovery.RepositoryRoot(directory)
	if repository == "" {
		return "", errors.New("no repository found; select configuration with --config")
	}
	directory = filepath.Join(repository, ".github")
	if err := configplugins.ValidateLocalPath(directory); err != nil {
		return "", err
	}
	return configplugins.FindOrDefaultPath(directory)
}

func OpenProject(ctx context.Context, rules config.RuleSet, acquisition Acquisition, directory, configPath string) (*Project, error) {
	path, err := projectConfigPath(directory, configPath)
	if err != nil {
		return nil, err
	}
	manifest, err := configplugins.Open(ctx, rules, path, acquisition.Connections)
	if err != nil {
		return nil, err
	}
	return &Project{Manifest: manifest, Releases: ResolvedPlugins{Plugins: []Pin{}}, StatusOptions: StatusOptions{Acquisition: acquisition}}, nil
}

// SelectedPlugins resolves declared releases without changing configuration.
func (p *Project) SelectedPlugins(ctx context.Context) (ResolvedPlugins, error) {
	return p.selectedPins(ctx, p.Manifest.References)
}

func (p *Project) selectedPins(ctx context.Context, refs []pluginref.Reference) (ResolvedPlugins, error) {
	selected := ResolvedPlugins{Plugins: []Pin{}}
	for _, ref := range refs {
		if source := ref.DirectSource(); source != "" {
			continue
		}
		if digest := ref.CandidateDigest(); digest != "" {
			client, err := NewWithGrant(ref.Registry(), p.StatusOptions)
			if err != nil {
				return ResolvedPlugins{}, err
			}
			pin, err := client.LookupCandidate(ctx, ref.Name(), ref.Version(), digest)
			if err != nil {
				return ResolvedPlugins{}, err
			}
			selected.Plugins = append(selected.Plugins, pin)
			continue
		}
		client, err := NewWithGrant(ref.Registry(), p.StatusOptions)
		if err != nil {
			return ResolvedPlugins{}, err
		}
		var pin Pin
		if p.defaultPlugins {
			pin, err = client.ResolveCached(ctx, ref.Name(), ref.Version())
		} else {
			pin, err = client.Lookup(ctx, ref.Name(), ref.Version())
		}
		if err != nil {
			return ResolvedPlugins{}, err
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
		candidate := strings.HasPrefix(ref.Registry(), "file:")
		updating := slices.Contains(update, ref)
		switch {
		case candidate && !updating && ref.CandidateDigest() != "":
			pin, err = client.LookupCandidate(ctx, ref.Name(), ref.Version(), ref.CandidateDigest())
		case candidate:
			// Explicit install resolves the current staging bytes. Only update can
			// replace an existing bound digest.
			pin, err = client.Resolve(ctx, ref.Name(), ref.Version())
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
		if err := p.Releases.CheckResolved(pin, ref); err != nil {
			return err
		}
		if err := p.checkConfiguredRelease(ctx, pin, ref); err != nil {
			return err
		}
		selected = append(selected, pin)
	}
	next := ResolvedPlugins{Plugins: selected}
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
	p.Releases = next
	return nil
}

// Configured releases retain publisher, immutable-version and downgrade checks.
// Authority evidence comes from cached or acquired registry records.
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
		baseline := ResolvedPlugins{Plugins: []Pin{previous}}
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
		for _, pin := range p.Releases.Plugins {
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

// Save writes the exact plugin declarations after checking configuration for concurrent edits.
func (p *Project) Save(ctx context.Context, refs []pluginref.Reference) error {
	resolved, err := p.ResolvedDeclarations(ctx, refs)
	if err != nil {
		return err
	}
	return p.Manifest.Save(ctx, resolved)
}

func (p *Project) artifactOptions() artifacts.Options {
	options := p.StatusOptions.Acquisition.ArtifactOptions()
	options.Network = p.StatusOptions.Network
	return options
}
