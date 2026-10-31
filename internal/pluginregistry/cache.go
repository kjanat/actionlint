package pluginregistry

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/pluginref"
)

// Lookup resolves a canonical exact release, reusing a previously verified
// registry resolution without network access. Floating requests belong to
// explicit install/update, which call Resolve before writing configuration.
func (c *Client) Lookup(ctx context.Context, name, version string) (Pin, error) {
	if !pluginref.ValidRepository(name) || !pluginref.ValidVersion(version) || version == "latest" {
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
	pin, err = c.Resolve(ctx, name, version)
	return pin, ownedLookupError(err)
}

// A lookup reports immutable failure data rather than exposing an HTTP
// transport's retained request/error objects. Preserve domain classifications.
type releaseLookupError struct {
	message                         string
	canceled, deadline, unavailable bool
}

func (e releaseLookupError) Error() string { return e.message }
func (e releaseLookupError) Is(target error) bool {
	return e.canceled && target == context.Canceled || e.deadline && target == context.DeadlineExceeded || e.unavailable && target == errReleaseUnavailable
}

func ownedLookupError(err error) error {
	if err == nil {
		return nil
	}
	return releaseLookupError{message: err.Error(), canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded), unavailable: errors.Is(err, errReleaseUnavailable)}
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
	data, err := readLockBytes(ctx, path)
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
