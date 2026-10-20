package pluginregistry

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"golang.org/x/mod/semver"
)

const LockName = "actionlint.plugins.lock.json"

type Lock struct {
	SchemaVersion int   `json:"schema_version"`
	Plugins       []Pin `json:"plugins"`
}

func (l Lock) Validate() error {
	if l.SchemaVersion != 1 || l.Plugins == nil {
		return errors.New("unsupported plugin lock schema")
	}
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

// LockFile retains the read bytes for a compare-before-replace transaction.
// Downloads finish before Save; failure leaves the previous lock untouched.
type LockFile struct {
	Path     string
	Lock     Lock
	original []byte
}

func readLockBytes(ctx context.Context, path string) ([]byte, error) {
	if err := safePath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJSONBytes {
		return nil, errors.New("plugin lock must be a regular file of at most 4 MiB")
	}
	f, err := fsread.New(ctx, nil, nil).Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJSONBytes {
		return nil, errors.New("plugin lock must be a regular file of at most 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJSONBytes+1))
	if len(data) > maxJSONBytes {
		return nil, errors.New("plugin lock exceeds 4 MiB")
	}
	return data, err
}

func OpenLock(ctx context.Context, path string) (*LockFile, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute, err = physicalLockPath(absolute)
	if err != nil {
		return nil, err
	}
	return openLock(ctx, absolute)
}

// OpenProjectLock uses the repository boundary selected by the caller. Never
// rediscover it from .github: a link there could itself point at another repo.
func OpenProjectLock(ctx context.Context, root string) (*LockFile, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	return openLock(ctx, filepath.Join(physical, ".github", LockName))
}

func openLock(ctx context.Context, absolute string) (*LockFile, error) {
	data, err := readLockBytes(ctx, absolute)
	if err != nil {
		return nil, err
	}
	f := &LockFile{Path: absolute, original: data, Lock: Lock{SchemaVersion: 1, Plugins: []Pin{}}}
	if data == nil {
		return f, nil
	}
	f.Lock = Lock{}
	if err := jsonv2.Unmarshal(data, &f.Lock, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("plugin lock %s: %w", absolute, err)
	}
	if err := f.Lock.Validate(); err != nil {
		return nil, fmt.Errorf("plugin lock %s: %w", absolute, err)
	}
	return f, nil
}

func (f *LockFile) Exists() bool { return f.original != nil }

// CheckUnchanged verifies the destination's safe ancestry and original bytes
// before a caller creates transaction guards or modifies a related manifest.
func (f *LockFile) CheckUnchanged(ctx context.Context) error {
	_, err := f.unchangedBytes(ctx)
	return err
}

func (f *LockFile) unchangedBytes(ctx context.Context) ([]byte, error) {
	current, err := readLockBytes(ctx, f.Path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, f.original) || (current == nil) != (f.original == nil) {
		return nil, errors.New("plugin lock changed during operation; retry")
	}
	return current, nil
}

// An explicit lock chooses its containing directory as the boundary. Resolve
// aliases of that directory; never resolve the lock file itself. Automatic
// project selection uses OpenProjectLock with its already-selected boundary.
func physicalLockPath(absolute string) (string, error) {
	base := filepath.Dir(absolute)
	for {
		_, err := os.Lstat(base)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(base) == base {
			return "", err
		}
		base = filepath.Dir(base)
	}
	physical, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil {
		return "", err
	}
	return filepath.Join(physical, relative), nil
}

// safePath refuses links in the destination and all existing ancestors. In
// particular a checked-in .github symlink cannot redirect lock mutations.
func safePath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin lock symlink refused: %s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func (f *LockFile) Save(ctx context.Context) (err error) {
	if err := f.Lock.Validate(); err != nil {
		return err
	}
	slices.SortFunc(f.Lock.Plugins, func(a, b Pin) int { return strings.Compare(a.Name, b.Name) })
	data, err := json.MarshalIndent(f.Lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxJSONBytes {
		return errors.New("plugin lock exceeds 4 MiB")
	}
	return f.replace(ctx, data)
}

// Restore rolls back this instance's last write after checking its written bytes.
// Edits observed before the final filesystem operation abort the rollback.
func (f *LockFile) Restore(ctx context.Context, previous *LockFile) error {
	if f.Path != previous.Path {
		return errors.New("cannot restore a different plugin lock")
	}
	if err := f.replace(ctx, previous.original); err != nil {
		return err
	}
	f.Lock = previous.Lock
	return nil
}

func (f *LockFile) replace(ctx context.Context, data []byte) (err error) {
	if err := safePath(f.Path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	guardPath := f.Path + ".writing"
	if err := os.Mkdir(guardPath, 0o700); err != nil {
		return fmt.Errorf("plugin lock busy (%s): %w", guardPath, err)
	}
	defer func() { err = errors.Join(err, os.Remove(guardPath)) }()
	current, err := f.unchangedBytes(ctx)
	if err != nil {
		return err
	}
	if bytes.Equal(current, data) {
		return nil
	}
	if data == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f.CheckUnchanged(ctx); err != nil {
			return err
		}
		if err := os.Remove(f.Path); err != nil {
			return err
		}
		f.original = nil
		return nil
	}
	directory, err := os.OpenRoot(guardPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	temp, err := directory.OpenFile("content", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		// A copied read-only mode must not prevent cleanup of our temporary file.
		if e := os.Chmod(temp.Name(), 0o600); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
		if e := os.Remove(temp.Name()); !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}()
	if current != nil {
		info, err := os.Lstat(f.Path)
		if err != nil {
			return errors.Join(err, temp.Close())
		}
		if err := temp.Chmod(info.Mode().Perm()); err != nil {
			return errors.Join(err, temp.Close())
		}
	}
	if _, err := temp.Write(data); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), f.Path); err != nil {
		return err
	}
	f.original = data
	return nil
}

// Set preserves publisher identity and refuses replacement of an accepted
// version, even if the registry starts serving a different artifact for it.
// Artifact format and filename may evolve across versions of the same plugin.
func (f *LockFile) Set(pin Pin) error {
	return f.set(pin, "")
}

// SetResolved accepts a registry change selected by the dependency declaration.
// Publisher identity and accepted immutable artifacts remain protected.
func (f *LockFile) SetResolved(pin Pin, ref pluginref.Reference) error {
	selected, err := pluginref.New(ref.Name, ref.Version, ref.Registry)
	if err != nil {
		return err
	}
	if !strings.EqualFold(pin.Name, selected.Name) || pin.Selector != selected.Version || !SameRegistry(pin.Registry, selected.Registry) {
		return fmt.Errorf("resolved pin does not match selected dependency %s", selected.String())
	}
	return f.set(pin, selected.Registry)
}

func (f *LockFile) set(pin Pin, selectedRegistry string) error {
	if err := pin.Validate(); err != nil {
		return err
	}
	for i, old := range f.Lock.Plugins {
		if pin.Alias != "" && old.Alias == pin.Alias && SameRegistry(old.Registry, pin.Registry) && !strings.EqualFold(old.Name, pin.Name) {
			return fmt.Errorf("registry alias %q changed from %s to %s; remove explicitly before switching", pin.Alias, old.Name, pin.Name)
		}
		if !strings.EqualFold(old.Name, pin.Name) {
			continue
		}
		if (!SameRegistry(old.Registry, pin.Registry) && !SameRegistry(selectedRegistry, pin.Registry)) || old.RepositoryID != pin.RepositoryID || old.OwnerID != pin.OwnerID || old.PluginID != pin.PluginID {
			return fmt.Errorf("publisher or plugin identity changed for %s; remove explicitly before switching", pin.Name)
		}
		if old.Version == pin.Version && (old.SHA256 != pin.SHA256 || old.URL != pin.URL || old.Contract != pin.Contract || old.Kind != pin.Kind || old.Asset != pin.Asset) {
			return fmt.Errorf("immutable version %s@%s changed", pin.Name, pin.Version)
		}
		previous := "v" + strings.TrimPrefix(old.Version, "v")
		next := "v" + strings.TrimPrefix(pin.Version, "v")
		if pin.Selector == "latest" && semver.IsValid(previous) && semver.Compare(next, previous) < 0 {
			return fmt.Errorf("latest would downgrade %s from %s to %s; select an exact --version to downgrade", pin.Name, old.Version, pin.Version)
		}
		f.Lock.Plugins[i] = pin
		return f.Lock.Validate()
	}
	f.Lock.Plugins = append(f.Lock.Plugins, pin)
	return f.Lock.Validate()
}

func (f *LockFile) Remove(name string) error {
	for i, p := range f.Lock.Plugins {
		if strings.EqualFold(p.Name, name) {
			f.Lock.Plugins = slices.Delete(f.Lock.Plugins, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("plugin %q is not installed", name)
}

// Sync restores exact pins without registry access or guest startup. Cached
// bytes are re-hashed by artifact.Resolve and work without network access.
func (l Lock) Sync(ctx context.Context, options artifact.Options) error {
	if err := l.CheckStatuses(ctx, StatusOptions{}); err != nil {
		return err
	}
	for _, p := range l.Plugins {
		a, err := artifact.Resolve(ctx, p.Source(), options)
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
		if err := p.Verify(a.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
	}
	return nil
}
