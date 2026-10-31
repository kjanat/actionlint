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
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"golang.org/x/mod/semver"
)

const LockName = "actionlint.plugins.lock.json"

// LockSchemaVersion is the lock schema version Save writes. Locks of schema
// version 1, which have no Sources, are read and rewritten as version 2.
const LockSchemaVersion = 2

type Lock struct {
	SchemaVersion int   `json:"schema_version"`
	Plugins       []Pin `json:"plugins"`
	// Sources lists the oci:// plugin sources declared directly in
	// configuration, with the image manifest and layer digests of every
	// platform they serve. Schema version 2 added it.
	Sources []SourcePin `json:"sources,omitempty"`
}

// SourcePin records what one oci:// source holds, so the cache plan and
// plugin sync know every blob of this host's platform without network
// access. In the lock it is {"source": ..., "platforms": {...}}.
type SourcePin struct {
	// source is oci://<host>/<repository>@sha256:<hex>. A tag is refused.
	source string
	// platforms maps "<os>/<arch>", or artifact.AnyPlatform for a lone
	// image manifest, to the image manifest and layer digests read there.
	platforms map[string]artifact.OCIPlatform
}

// sourcePinJSON is the lock representation of a SourcePin.
type sourcePinJSON struct {
	Source    string                          `json:"source"`
	Platforms map[string]artifact.OCIPlatform `json:"platforms"`
}

var platformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)

// NewSourcePin returns the pin of the oci:// source, which serves
// platforms. It checks that source is a normalized oci:// digest reference
// and that every platform lists valid digests.
func NewSourcePin(source string, platforms map[string]artifact.OCIPlatform) (SourcePin, error) {
	normalized, _, err := artifact.NormalizeSource(source, "")
	if err != nil {
		return SourcePin{}, fmt.Errorf("plugin lock source %q: %w", source, err)
	}
	if !artifact.IsOCI(source) || normalized != source {
		return SourcePin{}, fmt.Errorf("plugin lock source %q must be a normalized oci:// digest reference", source)
	}
	if len(platforms) == 0 {
		return SourcePin{}, fmt.Errorf("plugin lock source %s lists no platforms", source)
	}
	pin := SourcePin{source: source, platforms: make(map[string]artifact.OCIPlatform, len(platforms))}
	for platform, entry := range platforms {
		if platform != artifact.AnyPlatform && !platformPattern.MatchString(platform) {
			return SourcePin{}, fmt.Errorf("plugin lock source %s: invalid platform %q", source, platform)
		}
		if !digestPattern.MatchString(entry.Manifest) || len(entry.Blobs) == 0 {
			return SourcePin{}, fmt.Errorf("plugin lock source %s: platform %s needs a manifest digest and blobs", source, platform)
		}
		for _, blob := range entry.Blobs {
			if !digestPattern.MatchString(blob) {
				return SourcePin{}, fmt.Errorf("plugin lock source %s: platform %s: invalid blob digest %q", source, platform, blob)
			}
		}
		pin.platforms[platform] = artifact.OCIPlatform{Manifest: entry.Manifest, Blobs: slices.Clone(entry.Blobs)}
	}
	return pin, nil
}

// Source returns the oci:// source the pin describes.
func (p SourcePin) Source() string { return p.source }

// Digests returns every stored blob this host reads for the source: the
// root index or manifest, the image manifest of platform, falling back to
// artifact.AnyPlatform, and its layers. It reports false when the source
// serves neither.
func (p SourcePin) Digests(platform string) ([]string, bool) {
	entry, ok := p.platforms[platform]
	if !ok {
		entry, ok = p.platforms[artifact.AnyPlatform]
	}
	if !ok {
		return nil, false
	}
	_, root, _ := strings.Cut(p.source, "@sha256:")
	digests := make([]string, 0, 2+len(entry.Blobs))
	digests = append(digests, root, entry.Manifest)
	return append(digests, entry.Blobs...), true
}

// Verify checks that a pulled artifact read the image manifest and layers
// the pin records for its platform.
func (p SourcePin) Verify(info artifact.Info) error {
	entry, ok := p.platforms[info.Platform]
	if !ok || entry.Manifest != info.Manifest || len(entry.Blobs) != len(info.Blobs) {
		return fmt.Errorf("plugin %s: pulled image manifest %s disagrees with the plugin lock", p.source, info.Manifest)
	}
	for i, blob := range entry.Blobs {
		if info.Blobs[i] != blob {
			return fmt.Errorf("plugin %s: pulled layer %s disagrees with the plugin lock", p.source, info.Blobs[i])
		}
	}
	return nil
}

// MarshalJSON writes the lock representation.
func (p SourcePin) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(sourcePinJSON{Source: p.source, Platforms: p.platforms})
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return bytes.Clone(data), nil
}

// clone returns a copy that shares no storage with p.
func (p SourcePin) clone() SourcePin {
	platforms := make(map[string]artifact.OCIPlatform, len(p.platforms))
	for platform, entry := range p.platforms {
		platforms[platform] = artifact.OCIPlatform{Manifest: entry.Manifest, Blobs: slices.Clone(entry.Blobs)}
	}
	return SourcePin{source: p.source, platforms: platforms}
}

// UnmarshalJSON reads the lock representation and validates it like
// NewSourcePin.
func (p *SourcePin) UnmarshalJSON(data []byte) error {
	var raw sourcePinJSON
	if err := jsonv2.Unmarshal(data, &raw, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	pin, err := NewSourcePin(raw.Source, raw.Platforms)
	if err != nil {
		return err
	}
	*p = pin
	return nil
}

// SourcePin returns the pin of the oci:// source.
func (l Lock) SourcePin(source string) (SourcePin, bool) {
	for _, pin := range l.Sources {
		if pin.source == source {
			return pin.clone(), true
		}
	}
	return SourcePin{}, false
}

func (l Lock) Validate() error {
	if l.SchemaVersion != 1 && l.SchemaVersion != LockSchemaVersion || l.Plugins == nil {
		return errors.New("unsupported plugin lock schema")
	}
	if l.SchemaVersion == 1 && l.Sources != nil {
		return errors.New("plugin lock schema 1 has no sources; use schema 2")
	}
	sources := map[string]bool{}
	for _, p := range l.Sources {
		if p.source == "" {
			return errors.New("plugin lock source is empty; build it with NewSourcePin")
		}
		if sources[p.source] {
			return fmt.Errorf("duplicate plugin lock source %s", p.source)
		}
		sources[p.source] = true
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

// LockFile compares the bytes read at load time before replacing a lockfile.
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
	f := &LockFile{Path: absolute, original: data, Lock: Lock{SchemaVersion: LockSchemaVersion, Plugins: []Pin{}}}
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

// Save writes the lock as schema version LockSchemaVersion.
func (f *LockFile) Save(ctx context.Context) (err error) {
	f.Lock.SchemaVersion = LockSchemaVersion
	if err := f.Lock.Validate(); err != nil {
		return err
	}
	slices.SortFunc(f.Lock.Plugins, func(a, b Pin) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(f.Lock.Sources, func(a, b SourcePin) int { return strings.Compare(a.source, b.source) })
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
	if err := f.checkReplacement(pin, ""); err != nil {
		return err
	}
	return f.set(pin)
}

// SetResolved accepts a registry change selected by the dependency declaration.
func (f *LockFile) SetResolved(pin Pin, ref pluginref.Reference) error {
	if err := f.CheckResolved(pin, ref); err != nil {
		return err
	}
	return f.set(pin)
}

// CheckResolved verifies a selected pin against the original lock entries.
// Project resolution validates the final selection's uniqueness separately.
func (f *LockFile) CheckResolved(pin Pin, ref pluginref.Reference) error {
	selected, err := pluginref.New(ref.Name, ref.Version, ref.Registry)
	if err != nil {
		return err
	}
	if !pin.Matches(selected) || pin.Selector != selected.Version {
		return fmt.Errorf("resolved pin does not match selected dependency %s", selected.String())
	}
	return f.checkReplacement(pin, selected.Registry)
}

func (f *LockFile) checkReplacement(pin Pin, selectedRegistry string) error {
	if err := pin.Validate(); err != nil {
		return err
	}
	for _, old := range f.Lock.Plugins {
		if pin.Alias != "" && old.Alias == pin.Alias && SameRegistry(old.Registry, pin.Registry) && !strings.EqualFold(old.Name, pin.Name) {
			return fmt.Errorf("registry alias %q changed from %s to %s; remove explicitly before switching", pin.Alias, old.Name, pin.Name)
		}
		if !strings.EqualFold(old.Name, pin.Name) {
			continue
		}
		if (!SameRegistry(old.Registry, pin.Registry) && !SameRegistry(selectedRegistry, pin.Registry)) || old.RepositoryID != pin.RepositoryID || old.OwnerID != pin.OwnerID || old.PluginID != pin.PluginID {
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

func (f *LockFile) set(pin Pin) error {
	for i, old := range f.Lock.Plugins {
		if strings.EqualFold(old.Name, pin.Name) {
			f.Lock.Plugins[i] = pin
			return f.Lock.Validate()
		}
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
		if p.Origin == "candidate" {
			if err := p.importCandidate(ctx, options); err != nil {
				return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
			}
		}
		a, err := artifact.Resolve(ctx, p.Source(), options)
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
		if err := p.Verify(a.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", p.Name, p.Version, err)
		}
	}
	for _, p := range l.Sources {
		a, err := artifact.Resolve(ctx, p.source, options)
		if err != nil {
			return err
		}
		if err := p.Verify(a.Info); err != nil {
			return err
		}
	}
	return nil
}
