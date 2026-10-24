package pluginregistry

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/pluginref"
)

type ReleaseState string

var errReleaseUnavailable = errors.New("plugin release unavailable")

const (
	ReleaseActive     ReleaseState = "active"
	ReleaseDeprecated ReleaseState = "deprecated"
	ReleaseWithdrawn  ReleaseState = "withdrawn"
	ReleaseRevoked    ReleaseState = "revoked"
)

type StatusActor struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Workflow string `json:"workflow"`
	RunID    string `json:"run_id"`
}

type ReleaseStatus struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	SHA256      string       `json:"sha256"`
	Status      ReleaseState `json:"status"`
	Reason      string       `json:"reason"`
	Replacement string       `json:"replacement,omitempty"`
	ChangedAt   time.Time    `json:"changed_at"`
	Actor       StatusActor  `json:"actor"`
}

type StatusSnapshot struct {
	SchemaVersion int             `json:"schema_version"`
	Releases      []ReleaseStatus `json:"releases"`
}

type StatusOptions struct {
	// CacheDir defaults to the user's actionlint/registry cache.
	CacheDir string
	Warning  func(string)
}

func (o StatusOptions) warn(message string) {
	if o.Warning != nil {
		o.Warning(message)
	}
}

func (s ReleaseStatus) validate() error {
	switch s.Status {
	case ReleaseActive, ReleaseDeprecated, ReleaseWithdrawn, ReleaseRevoked:
	default:
		return errors.New("unknown registry release status")
	}
	if !canonicalName(s.Name) || !pluginref.ValidVersion(s.Version) || !digestPattern.MatchString(s.SHA256) ||
		strings.TrimSpace(s.Reason) == "" || len(s.Reason) > 2048 || len(s.Replacement) > 512 || s.ChangedAt.IsZero() ||
		s.Actor.Kind != "registry-admin" || !numericID(s.Actor.ID) || s.Actor.Name == "" || s.Actor.Workflow == "" || s.Actor.RunID == "" {
		return errors.New("invalid registry release status identity or audit")
	}
	return nil
}

func (s StatusSnapshot) validate() error {
	if s.SchemaVersion != 1 || s.Releases == nil {
		return errors.New("unsupported registry status snapshot")
	}
	seen := map[string]bool{}
	for _, status := range s.Releases {
		if err := status.validate(); err != nil {
			return err
		}
		key := strings.ToLower(status.Name) + "\n" + status.Version
		if seen[key] {
			return errors.New("duplicate registry release status")
		}
		seen[key] = true
	}
	return nil
}

func statusDirectory(registry string, options StatusOptions) (string, error) {
	endpoint, err := registryURL(registry)
	if err != nil {
		return "", err
	}
	endpoint.Host = strings.ToLower(endpoint.Host)
	base := options.CacheDir
	if base == "" {
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(base, "actionlint", "registry")
	}
	absolute, err := filepath.Abs(filepath.Join(base, hash(endpoint.String())))
	if err != nil {
		return "", err
	}
	return physicalLockPath(absolute)
}

func readStatusSnapshot(ctx context.Context, directory string) (StatusSnapshot, error) {
	data, err := readLockBytes(ctx, filepath.Join(directory, "statuses.json"))
	if err != nil || data == nil {
		return StatusSnapshot{SchemaVersion: 1, Releases: []ReleaseStatus{}}, err
	}
	var snapshot StatusSnapshot
	if err := jsonv2.Unmarshal(data, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, snapshot.validate()
}

func readRevocation(ctx context.Context, directory, digest string) (*ReleaseStatus, error) {
	data, err := readLockBytes(ctx, filepath.Join(directory, "revoked", digest+".json"))
	if err != nil || data == nil {
		return nil, err
	}
	var status ReleaseStatus
	if err := jsonv2.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	if err := status.validate(); err != nil {
		return nil, err
	}
	if status.SHA256 != digest || status.Status != ReleaseRevoked {
		return nil, errors.New("cached revocation identity mismatch")
	}
	return &status, nil
}

func writeStatusFile(path string, value any, immutable bool) (err error) {
	if err := safePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err := json.NewEncoder(file).Encode(value); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if immutable {
		// Publish a complete record atomically without ever replacing one written
		// by another process. Losing a later snapshot cannot erase revocation.
		return os.Link(file.Name(), path)
	}
	return os.Rename(file.Name(), path)
}

// RefreshStatuses fetches the registry's current sidecar and persists every
// observed revocation before replacing its mutable snapshot.
func (c *Client) RefreshStatuses(ctx context.Context) (StatusSnapshot, error) {
	if c.base.Scheme == "file" {
		return StatusSnapshot{SchemaVersion: 1, Releases: []ReleaseStatus{}}, ctx.Err()
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var snapshot StatusSnapshot
	if err := get(bounded, c, "statuses.json", &snapshot); err != nil {
		return snapshot, err
	}
	if err := snapshot.validate(); err != nil {
		return snapshot, err
	}
	directory, err := statusDirectory(c.base.String(), c.StatusOptions)
	if err != nil {
		return snapshot, err
	}
	for _, status := range snapshot.Releases {
		if status.Status != ReleaseRevoked {
			continue
		}
		path := filepath.Join(directory, "revoked", status.SHA256+".json")
		if err := writeStatusFile(path, status, true); err != nil && !errors.Is(err, os.ErrExist) {
			return snapshot, err
		}
		if _, err := readRevocation(ctx, directory, status.SHA256); err != nil {
			return snapshot, err
		}
	}
	return snapshot, writeStatusFile(filepath.Join(directory, "statuses.json"), snapshot, false)
}

func statusMessage(pin Pin, status ReleaseStatus) string {
	message := fmt.Sprintf("plugin %s@%s is %s: %s", pin.Name, pin.Version, status.Status, status.Reason)
	if status.Replacement != "" {
		message += "; replacement: " + status.Replacement
	}
	return message
}

func checkStatus(ctx context.Context, pin Pin, snapshot StatusSnapshot, options StatusOptions, fresh bool) error {
	directory, err := statusDirectory(pin.Registry, options)
	if err != nil {
		return err
	}
	if revoked, err := readRevocation(ctx, directory, pin.SHA256); err != nil {
		return err
	} else if revoked != nil {
		return fmt.Errorf("%w: %s", errReleaseUnavailable, statusMessage(pin, *revoked))
	}
	for _, status := range snapshot.Releases {
		if status.Status == ReleaseRevoked && status.SHA256 == pin.SHA256 {
			return fmt.Errorf("%w: %s", errReleaseUnavailable, statusMessage(pin, status))
		}
		if !strings.EqualFold(status.Name, pin.Name) || status.Version != pin.Version {
			continue
		}
		if status.SHA256 != pin.SHA256 {
			return errors.New("registry release status disagrees with pinned artifact digest")
		}
		switch status.Status {
		case ReleaseRevoked:
			return fmt.Errorf("%w: %s", errReleaseUnavailable, statusMessage(pin, status))
		case ReleaseWithdrawn:
			if fresh {
				return fmt.Errorf("%w: %s", errReleaseUnavailable, statusMessage(pin, status))
			}
			options.warn(statusMessage(pin, status))
		case ReleaseDeprecated:
			options.warn(statusMessage(pin, status))
		}
	}
	return nil
}

// CheckStatuses never contacts the registry. Known revocations block cached
// bytes; withdrawal does not invalidate an existing lock.
func (l Lock) CheckStatuses(ctx context.Context, options StatusOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := l.Validate(); err != nil {
		return err
	}
	for _, pin := range l.Plugins {
		if pin.Origin == "candidate" {
			continue
		}
		directory, err := statusDirectory(pin.Registry, options)
		if err != nil {
			return err
		}
		snapshot, err := readStatusSnapshot(ctx, directory)
		if err != nil {
			return err
		}
		if err := checkStatus(ctx, pin, snapshot, options, false); err != nil {
			return err
		}
	}
	return nil
}

// RefreshStatuses is for explicit install/update/sync operations. Unavailable
// registries produce warnings and retain cached policy, including revocations.
func (l Lock) RefreshStatuses(ctx context.Context, options StatusOptions) error {
	if err := l.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, pin := range l.Plugins {
		if pin.Origin == "candidate" {
			continue
		}
		directory, err := statusDirectory(pin.Registry, options)
		if err != nil {
			return err
		}
		if seen[directory] {
			continue
		}
		seen[directory] = true
		client, err := New(pin.Registry)
		if err != nil {
			return err
		}
		client.StatusOptions = options
		if _, err := client.RefreshStatuses(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			options.warn(fmt.Sprintf("could not refresh plugin statuses from %s; using cached status: %v", pin.Registry, err))
		}
	}
	return l.CheckStatuses(ctx, options)
}
