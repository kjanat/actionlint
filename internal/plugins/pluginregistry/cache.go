package pluginregistry

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func (c *Client) ResolveCached(ctx context.Context, name, selector string) (Pin, error) {
	if !pluginreference.ValidName(name) && !pluginreference.ValidAlias(name) || !pluginreference.ValidVersion(selector) {
		return Pin{}, errors.New("invalid plugin name or version selector")
	}
	shared, host, err := c.selectionPaths(name, selector)
	if err != nil {
		return Pin{}, err
	}
	if !network.Settings(ctx).WarmCache || network.Settings(ctx).Offline {
		for _, path := range []string{host, shared} {
			selected, found, err := c.cachedSelection(ctx, path, name, selector)
			if err != nil || found {
				return selected, err
			}
		}
	}
	if network.Settings(ctx).Offline {
		return Pin{}, network.Missing(name + "@" + selector)
	}
	pin, fallback, err := c.resolveSelection(ctx, name, selector, false)
	if err != nil {
		return Pin{}, err
	}
	if fallback {
		return pin, writeCacheFile(host, pin, false)
	}
	if err := os.Remove(host); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Pin{}, err
	}
	return pin, writeCacheFile(shared, pin, false)
}

func (c *Client) cachedSelection(ctx context.Context, path, name, selector string) (Pin, bool, error) {
	data, err := readCacheBytes(ctx, path)
	if err != nil || data == nil {
		return Pin{}, false, err
	}
	var selected Pin
	if err := jsonv2.Unmarshal(data, &selected, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Pin{}, false, err
	}
	if err := selected.Validate(); err != nil {
		return Pin{}, false, err
	}
	if !SameRegistry(selected.Registry, c.base.String()) || selected.Name != name && selected.Alias != name || selected.Selector != selector {
		return Pin{}, false, errors.New("cached plugin selection disagrees with requested registry, name or selector")
	}
	pin, exists, err := c.cachedRelease(ctx, selected.Name, selected.Version)
	if err != nil {
		return Pin{}, false, err
	}
	if !exists || !sameRelease(pin, selected) {
		return Pin{}, false, errors.New("cached plugin selection has no matching verified release")
	}
	_, rejected, err := c.rejection(ctx, selected)
	if err != nil || rejected {
		return Pin{}, false, err
	}
	selected.Registry = pin.Registry
	return selected, true, nil
}

func selectionKey(name, selector, accepted string) string {
	return sha256hex.Digest([]byte(name + "\n" + selector + "\n" + accepted))
}

func (c *Client) selectionPaths(name, selector string) (string, string, error) {
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return "", "", err
	}
	directory = filepath.Join(directory, "selections")
	return filepath.Join(directory, selectionKey(name, selector, component.AcceptedInterfacesDigest())+".json"), filepath.Join(directory, selectionKey(name, selector, c.admissionIdentity())+".json"), nil
}

func (c *Client) admissionIdentity() string {
	if c.identity != "" {
		return c.identity
	}
	return component.AdmissionIdentity()
}

type rejectedRelease struct {
	SHA256 string `json:"sha256"`
	Reason string `json:"reason"`
}

func (c *Client) rejectionPath(digest string) (string, error) {
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "rejected", sha256hex.Digest([]byte(digest+"\n"+c.admissionIdentity()))+".json"), nil
}

func (c *Client) rejection(ctx context.Context, pin Pin) (string, bool, error) {
	path, err := c.rejectionPath(pin.SHA256)
	if err != nil {
		return "", false, err
	}
	data, err := readCacheBytes(ctx, path)
	if err != nil || data == nil {
		return "", false, err
	}
	var record rejectedRelease
	if err := jsonv2.Unmarshal(data, &record, jsonv2.RejectUnknownMembers(true)); err != nil {
		return "", false, err
	}
	if record.SHA256 != pin.SHA256 {
		return "", false, errors.New("rejected release record disagrees with its digest")
	}
	return record.Reason, true, nil
}

// ReselectDefault records a rejection and retries selection against the built-in registry.
func ReselectDefault(ctx context.Context, pin Pin, reason *component.AdmissionError, options StatusOptions) (Pin, error) {
	if registry, err := registryURL(pin.Registry); err != nil || registry.String() != DefaultURL {
		return Pin{}, errors.New("rejected default plugin pin belongs to another registry")
	}
	u := &url.URL{Scheme: "https", Host: pluginreference.DefaultRegistryHost, Path: "/"}
	c, err := newClient(u, options.Network, options)
	if err != nil {
		return Pin{}, err
	}
	return c.reselect(ctx, pin, reason)
}

func (c *Client) reselect(ctx context.Context, pin Pin, reason *component.AdmissionError) (Pin, error) {
	path, err := c.rejectionPath(pin.SHA256)
	if err != nil {
		return Pin{}, err
	}
	if err := writeCacheFile(path, rejectedRelease{SHA256: pin.SHA256, Reason: reason.Error()}, false); err != nil {
		return Pin{}, err
	}
	name := pin.Alias
	if name == "" {
		name = pin.Name
	}
	shared, host, err := c.selectionPaths(name, pin.Selector)
	if err != nil {
		return Pin{}, err
	}
	for _, selection := range []string{shared, host} {
		if err := os.Remove(selection); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Pin{}, err
		}
	}
	return c.ResolveCached(ctx, name, pin.Selector)
}

// Lookup reuses a previously verified registry resolution without network access.
func (c *Client) Lookup(ctx context.Context, name, version string) (Pin, error) {
	if c.base.Scheme == "file" {
		return Pin{}, errors.New("local candidate lookup requires a configured SHA256 digest; run actionlint plugin install to bind it")
	}
	if !pluginreference.ValidName(name) || !pluginreference.ValidVersion(version) || version == "latest" {
		return Pin{}, errors.New("registry plugins require a canonical package and exact version; run actionlint plugin install to resolve the declaration")
	}
	if err := ctx.Err(); err != nil {
		return Pin{}, err
	}
	pin, exists, err := c.cachedRelease(ctx, name, version)
	if err != nil {
		return Pin{}, flattenLookupError(err)
	}
	if exists {
		return pin, nil
	}
	if network.Settings(ctx).Offline {
		return Pin{}, network.Missing(name + "@" + version)
	}
	pin, err = c.Resolve(ctx, name, version)
	return pin, flattenLookupError(err)
}

type releaseLookupError struct {
	message                                  string
	canceled, deadline, unavailable, offline bool
}

func (e releaseLookupError) Error() string { return e.message }
func (e releaseLookupError) Is(target error) bool {
	return e.offline && target == network.ErrOffline || e.canceled && target == context.Canceled || e.deadline && target == context.DeadlineExceeded || e.unavailable && target == errReleaseUnavailable
}

func flattenLookupError(err error) error {
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
	return filepath.Join(directory, "releases", sha256hex.Digest([]byte(name+"\n"+version))+".json"), nil
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
	pin.Registry = c.base.String()
	return pin, true, nil
}

func (c *Client) LookupCandidate(ctx context.Context, name, version, digest string) (Pin, error) {
	if c.base.Scheme != "file" || !pluginreference.ValidName(name) || !pluginreference.ValidVersion(version) || version == "latest" || !digestPattern.MatchString(digest) {
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
		pin, err = c.resolveCandidate(ctx, name, version, true)
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

// RememberCandidate never changes a mutable name/version alias.
func (c *Client) RememberCandidate(ctx context.Context, pin Pin, options artifacts.Options) error {
	if c.base.Scheme != "file" || pin.Origin != "candidate" || !SameRegistry(pin.Registry, c.base.String()) {
		return errors.New("candidate pin belongs to another registry")
	}
	pin.Selector, pin.Roles = pin.Version, component.Roles{}
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
		if !exists || !sameRelease(previous, pin) {
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
	return filepath.Join(directory, "candidates", sha256hex.Digest([]byte(name+"\n"+version+"\n"+digest))+".json"), nil
}

func sameRelease(left, right Pin) bool {
	if left.Metadata != "" && right.Metadata != "" && !artifacts.SameMetadata([]byte(left.Metadata), []byte(right.Metadata)) {
		return false
	}
	left.Metadata, right.Metadata = "", ""
	if !SameRegistry(left.Registry, right.Registry) {
		return false
	}
	left.Registry = right.Registry
	left.Alias, right.Alias = "", ""
	left.Selector, right.Selector = left.Version, right.Version
	left.Contract, right.Contract = contractInterface(left.Contract), contractInterface(right.Contract)
	left.Roles, right.Roles = component.Roles{}, component.Roles{}
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
	pin.Selector, pin.Roles = pin.Version, component.Roles{}
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
