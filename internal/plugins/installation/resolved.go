package installation

import (
	"context"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
	"golang.org/x/mod/semver"
)

// ResolvedPlugins contains verified releases selected for an invocation.
type ResolvedPlugins struct {
	Plugins []Pin `json:"plugins"`
}

func (l ResolvedPlugins) Validate() error {
	names, ids := map[string]bool{}, map[string]bool{}
	aliases := map[string]bool{}
	for _, p := range l.Plugins {
		if err := p.Validate(); err != nil {
			return err
		}
		name := strings.ToLower(p.Name)
		if names[name] || ids[p.PluginID] {
			return fmt.Errorf("duplicate plugin repository or metadata ID: %s (%s)", p.Name, p.PluginID)
		}
		names[name], ids[p.PluginID] = true, true
		if p.Alias != "" {
			registry, _ := registryURL(p.Registry)
			registry.Host = strings.ToLower(registry.Host)
			key := registry.String() + "\n" + p.Alias
			if aliases[key] {
				return fmt.Errorf("duplicate plugin alias %q in registry %s", p.Alias, p.Registry)
			}
			aliases[key] = true
		}
	}
	return nil
}

func (l ResolvedPlugins) CheckResolved(pin Pin, ref pluginref.Reference) error {
	declaration, err := pluginref.New(ref.Name(), ref.Version(), ref.Registry())
	if err != nil {
		return err
	}
	if !pin.Matches(declaration) || pin.Selector != declaration.Version() {
		return fmt.Errorf("resolved pin does not match selected dependency %s", declaration.String())
	}
	return l.CheckReplacement(pin, ref.Registry())
}

// CheckReplacement preserves publisher identity and immutable release contents.
func (l ResolvedPlugins) CheckReplacement(pin Pin, selectedRegistry string) error {
	if err := pin.Validate(); err != nil {
		return err
	}
	for _, old := range l.Plugins {
		if pin.Alias != "" && old.Alias == pin.Alias && SameRegistry(old.Registry, pin.Registry) && !strings.EqualFold(old.Name, pin.Name) {
			return fmt.Errorf("registry alias %q changed from %s to %s; remove explicitly before switching", pin.Alias, old.Name, pin.Name)
		}
		if !strings.EqualFold(old.Name, pin.Name) {
			continue
		}
		if (!SameRegistry(old.Registry, pin.Registry) && !SameRegistry(selectedRegistry, pin.Registry)) || !strings.EqualFold(old.RepositoryName(), pin.RepositoryName()) || old.RepositoryID != pin.RepositoryID || old.OwnerID != pin.OwnerID || old.PluginID != pin.PluginID {
			return fmt.Errorf("publisher or plugin identity changed for %s; remove explicitly before switching", pin.Name)
		}
		if old.Origin != "candidate" && old.Version == pin.Version && (old.SHA256 != pin.SHA256 || old.URL != pin.URL || old.Contract != pin.Contract || old.Kind != pin.Kind || old.Asset != pin.Asset) {
			return fmt.Errorf("immutable version %s@%s changed", pin.Name, pin.Version)
		}
		previous := "v" + strings.TrimPrefix(old.Version, "v")
		next := "v" + strings.TrimPrefix(pin.Version, "v")
		if pin.Selector == "latest" && semver.IsValid(previous) && semver.Compare(next, previous) < 0 {
			return fmt.Errorf("latest would downgrade %s from %s to %s; select an exact --version to downgrade", pin.Name, old.Version, pin.Version)
		}
	}
	return nil
}

func (l ResolvedPlugins) Sync(ctx context.Context, options artifacts.Options, statuses StatusOptions) error {
	if err := l.CheckStatuses(ctx, statuses); err != nil {
		return err
	}
	for _, p := range l.Plugins {
		if p.Origin == "candidate" {
			if err := p.importCandidate(ctx, options); err != nil {
				return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
			}
		}
		a, err := artifacts.Resolve(ctx, p.Source(), options)
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
		if err := p.Verify(a.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
	}
	return nil
}
