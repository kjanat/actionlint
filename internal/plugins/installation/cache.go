package installation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// ResolveCached reuses the last verified release selected by a name and version
// selector. Cache warming refreshes the selection from the registry.
func (c *Client) ResolveCached(ctx context.Context, name, selector string) (Pin, error) {
	if !pluginref.ValidName(name) && !pluginref.ValidAlias(name) || !pluginref.ValidVersion(selector) {
		return Pin{}, errors.New("invalid plugin name or version selector")
	}
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return Pin{}, err
	}
	path := filepath.Join(directory, "selections", hash(name+"\n"+selector)+".json")
	if !network.Settings(ctx).WarmCache || network.Settings(ctx).Offline {
		data, err := readCacheBytes(ctx, path)
		if err != nil {
			return Pin{}, err
		}
		if data != nil {
			var selected Pin
			if err := jsonv2.Unmarshal(data, &selected, jsonv2.RejectUnknownMembers(true)); err != nil {
				return Pin{}, err
			}
			if err := selected.Validate(); err != nil {
				return Pin{}, err
			}
			if !SameRegistry(selected.Registry, c.base.String()) || selected.Name != name && selected.Alias != name || selected.Selector != selector {
				return Pin{}, errors.New("cached plugin selection disagrees with requested registry, name or selector")
			}
			pin, exists, err := c.cachedRelease(ctx, selected.Name, selected.Version)
			if err != nil {
				return Pin{}, err
			}
			if !exists || !sameRelease(pin, selected) {
				return Pin{}, errors.New("cached plugin selection has no matching verified release")
			}
			return selected, nil
		}
	}
	if network.Settings(ctx).Offline {
		return Pin{}, network.Missing(name + "@" + selector)
	}
	pin, err := c.Resolve(ctx, name, selector)
	if err != nil {
		return Pin{}, err
	}
	if err := writeCacheFile(path, pin, false); err != nil {
		return Pin{}, err
	}
	return pin, nil
}

// Lookup resolves a canonical exact release, reusing a previously verified
// registry resolution without network access. Floating requests belong to
// explicit install/update, which call Resolve before writing configuration.
func (c *Client) Lookup(ctx context.Context, name, version string) (Pin, error) {
	if c.base.Scheme == "file" {
		return Pin{}, errors.New("local candidate lookup requires a configured SHA256 digest; run actionlint plugin install to bind it")
	}
	if !pluginref.ValidName(name) || !pluginref.ValidVersion(version) || version == "latest" {
		return Pin{}, errors.New("registry plugins require a canonical package and exact version; run actionlint plugin install to resolve the declaration")
	}
	if err := ctx.Err(); err != nil {
		return Pin{}, err
	}
	pin, exists, err := c.cachedRelease(ctx, name, version)
	if err != nil {
		return Pin{}, ownedLookupError(err)
	}
	if exists {
		return pin, nil
	}
	if network.Settings(ctx).Offline {
		return Pin{}, network.Missing(name + "@" + version)
	}
	pin, err = c.Resolve(ctx, name, version)
	return pin, ownedLookupError(err)
}

// A lookup reports immutable failure data rather than exposing an HTTP
// transport's retained request/error objects. Preserve domain classifications.
type releaseLookupError struct {
	message                                  string
	canceled, deadline, unavailable, offline bool
}

func (e releaseLookupError) Error() string { return e.message }
func (e releaseLookupError) Is(target error) bool {
	return e.offline && target == network.ErrOffline || e.canceled && target == context.Canceled || e.deadline && target == context.DeadlineExceeded || e.unavailable && target == errReleaseUnavailable
}

func ownedLookupError(err error) error {
	if err == nil {
		return nil
	}
	return releaseLookupError{message: err.Error(), offline: network.OnlyOffline(err), canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded), unavailable: errors.Is(err, errReleaseUnavailable)}
}

func (c *Client) releasePath(name, version string) (string, error) {
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "releases", hash(name+"\n"+version)+".json"), nil
}

func (c *Client) cachedRelease(ctx context.Context, name, version string) (Pin, bool, error) {
	if c.base.Scheme == "file" {
		return Pin{}, false, nil
	}
	path, err := c.releasePath(name, version)
	if err != nil {
		return Pin{}, false, err
	}
	data, err := readCacheBytes(ctx, path)
	if err != nil || data == nil {
		return Pin{}, false, err
	}
	var pin Pin
	if err := jsonv2.Unmarshal(data, &pin, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Pin{}, false, err
	}
	if err := pin.Validate(); err != nil {
		return Pin{}, false, fmt.Errorf("cached registry release: %w", err)
	}
	if !SameRegistry(pin.Registry, c.base.String()) || pin.Name != name || pin.Version != version || pin.Selector != version || pin.Origin != "" {
		return Pin{}, false, errors.New("cached registry release disagrees with its authority or requested identity")
	}
	return pin, true, nil
}

// LookupCandidate returns only the previously selected bytes for an exact
// local candidate digest. An uncached selection may be restored from staging
// only when the staged artifact still has that digest.
func (c *Client) LookupCandidate(ctx context.Context, name, version, digest string) (Pin, error) {
	if c.base.Scheme != "file" || !pluginref.ValidName(name) || !pluginref.ValidVersion(version) || version == "latest" || !digestPattern.MatchString(digest) {
		return Pin{}, errors.New("candidate lookup requires a local registry, canonical package, exact version, and SHA256 digest")
	}
	if err := ctx.Err(); err != nil {
		return Pin{}, err
	}
	pin, exists, err := c.cachedCandidate(ctx, name, version, digest)
	if err != nil {
		return Pin{}, err
	}
	if !exists {
		pin, err = c.resolveCandidate(ctx, name, version)
		if err != nil {
			return Pin{}, fmt.Errorf("candidate %s@%s is not cached; restore its matching local staging artifact or run actionlint plugin update: %w", name, version, err)
		}
		if pin.SHA256 != digest {
			return Pin{}, fmt.Errorf("candidate %s@%s changed; run actionlint plugin update to select the rebuilt bytes", name, version)
		}
		pin.Selector = version
	}
	return pin, nil
}

// RememberCandidate stores verified candidate metadata under an immutable
// per-digest key. Callers supply the execution cache and a cache-only OCI
// resolver. It never changes a mutable name/version alias.
func (c *Client) RememberCandidate(ctx context.Context, pin Pin, options artifacts.Options) error {
	if c.base.Scheme != "file" || pin.Origin != "candidate" || !SameRegistry(pin.Registry, c.base.String()) {
		return errors.New("candidate pin belongs to another registry")
	}
	pin.Selector = pin.Version
	if err := pin.Validate(); err != nil {
		return err
	}
	artifact, err := artifacts.Resolve(ctx, pin.Source(), options)
	if err != nil {
		return fmt.Errorf("candidate artifact is not verified in the artifact cache: %w", err)
	}
	if err := pin.Verify(artifact.Info); err != nil {
		return err
	}
	path, err := c.candidateCachePath(pin.Name, pin.Version, pin.SHA256)
	if err != nil {
		return err
	}
	if err := writeCacheFile(path, pin, true); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		previous, exists, readErr := c.cachedCandidate(ctx, pin.Name, pin.Version, pin.SHA256)
		if readErr != nil {
			return readErr
		}
		if !exists || !sameCandidate(previous, pin) {
			return errors.New("concurrent immutable candidate cache conflict")
		}
	}
	return nil
}

func (c *Client) cachedCandidate(ctx context.Context, name, version, digest string) (Pin, bool, error) {
	path, err := c.candidateCachePath(name, version, digest)
	if err != nil {
		return Pin{}, false, err
	}
	data, err := readCacheBytes(ctx, path)
	if err != nil || data == nil {
		return Pin{}, false, err
	}
	var pin Pin
	if err := jsonv2.Unmarshal(data, &pin, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Pin{}, false, err
	}
	if err := pin.Validate(); err != nil {
		return Pin{}, false, fmt.Errorf("cached candidate pin: %w", err)
	}
	if pin.Origin != "candidate" || !SameRegistry(pin.Registry, c.base.String()) || pin.Name != name || pin.Version != version || pin.Selector != version || pin.SHA256 != digest {
		return Pin{}, false, errors.New("cached candidate pin disagrees with its authority or requested identity")
	}
	return pin, true, nil
}

func (c *Client) candidateCachePath(name, version, digest string) (string, error) {
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "candidates", hash(name+"\n"+version+"\n"+digest)+".json"), nil
}

func sameCandidate(left, right Pin) bool {
	left.Selector, right.Selector = left.Version, right.Version
	return left == right && SameRegistry(left.Registry, right.Registry)
}

func sameRelease(left, right Pin) bool {
	if !SameRegistry(left.Registry, right.Registry) {
		return false
	}
	left.Registry = right.Registry
	// Alias spelling and selection intent do not change a release's identity.
	left.Alias, right.Alias = "", ""
	left.Selector, right.Selector = left.Version, right.Version
	return left == right
}

func (c *Client) cacheRelease(ctx context.Context, pin Pin) error {
	if c.base.Scheme == "file" {
		return nil
	}
	if err := pin.Validate(); err != nil {
		return err
	}
	if !SameRegistry(pin.Registry, c.base.String()) {
		return errors.New("registry release belongs to another authority")
	}
	previous, exists, err := c.cachedRelease(ctx, pin.Name, pin.Version)
	if err != nil {
		return err
	}
	if exists {
		if !sameRelease(previous, pin) {
			return fmt.Errorf("immutable registry release %s@%s changed", pin.Name, pin.Version)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := c.releasePath(pin.Name, pin.Version)
	if err != nil {
		return err
	}
	pin.Selector = pin.Version
	if err := writeCacheFile(path, pin, true); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		// A concurrent writer may have published the same release first.
		previous, exists, readErr := c.cachedRelease(ctx, pin.Name, pin.Version)
		if readErr != nil {
			return readErr
		}
		if !exists || !sameRelease(previous, pin) {
			return errors.New("concurrent immutable registry release conflict")
		}
	}
	return nil
}
