package plugins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tree"
	"actionlint.kjanat.dev/v2/internal/input/files"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// ReadMode selects which configuration fields are normalized during plugin discovery.
type ReadMode uint8

const (
	// CompleteConfiguration normalizes every configuration field before merging.
	CompleteConfiguration ReadMode = iota
	// PluginDeclarationsOnly projects plugin declarations and inheritance before merging.
	PluginDeclarationsOnly
)

// MaxFileBytes limits each configuration source to four MiB.
const MaxFileBytes = 4 << 20

// File edits plugin declarations and checks inherited inputs for
// concurrent changes before saving.
type File struct {
	Path       string
	References []pluginref.Reference
	Declared   bool
	Exists     bool
	original   []byte
	snapshots  map[string][]byte
	node       any
	readSource func(context.Context, string) ([]byte, error)
}

// Open reads plugin declarations and the configuration sources they inherit. Plugin rule configuration
// cannot be validated until the plugins supplying those rules have been loaded.
func Open(ctx context.Context, rules config.RuleSet, path string, connections acquisitionhttp.Connections) (*File, error) {
	return Read(ctx, rules, path, func(ctx context.Context, path string) ([]byte, error) {
		return readPluginManifest(ctx, path, connections)
	}, CompleteConfiguration)
}

// Read loads plugin declarations with the supplied source reader and normalization mode.
func Read(ctx context.Context, rules config.RuleSet, path string, read func(context.Context, string) ([]byte, error), mode ReadMode) (*File, error) {
	absolute, err := address.Normalize(path)
	if err != nil {
		return nil, err
	}
	if address.Remote(absolute) {
		// Pass the declared URL spelling to caller-supplied readers.
		absolute = path
	} else {
		absolute, err = physicalPluginManifestPath(absolute)
		if err != nil {
			return nil, err
		}
	}
	manifest := &File{Path: absolute, snapshots: map[string][]byte{}, readSource: read}
	data, err := manifest.read(ctx, absolute)
	if errors.Is(err, os.ErrNotExist) {
		manifest.snapshots[absolute] = nil
		return manifest, nil
	}
	if err != nil {
		return nil, err
	}
	manifest.original, manifest.Exists = data, true
	manifest.snapshots[absolute] = bytes.Clone(data)
	loaded, err := tree.LoadWithMerge(absolute, func(name string) ([]byte, error) {
		if name == absolute {
			return bytes.Clone(data), nil
		}
		if !address.Remote(name) {
			name, err = filepath.Abs(name)
			if err != nil {
				return nil, err
			}
		}
		content, err := manifest.read(ctx, name)
		if err == nil {
			manifest.snapshots[name] = bytes.Clone(content)
		}
		return content, err
	}, func(origin string, doc document.Document) (document.Document, error) {
		if err := ValidateRoot(doc.Value); err != nil {
			return doc, err
		}
		if err := config.ValidateSourcePaths(origin, doc.Value); err != nil {
			return doc, err
		}
		var err error
		if mode == PluginDeclarationsOnly {
			doc.Value, err = pluginDependencyNode(doc.Value)
		} else {
			doc.Value, err = rules.NormalizeForMerge(doc.Value)
		}
		return doc, err
	}, config.MergeDocuments)
	if err != nil {
		return nil, err
	}
	manifest.original = manifest.snapshots[absolute]
	manifest.node = loaded.Value
	manifest.References, manifest.Declared, err = References(loaded.Value)
	if err != nil {
		return nil, fmt.Errorf("plugins in %s: %w", absolute, err)
	}
	return manifest, nil
}

// The tree loader expands aliases before this projection, so plugin references
// may still use anchors declared under unrelated fields. Validate every source
// declaration before merging: a leaf reset cannot hide a malformed base.
func pluginDependencyNode(value any) (any, error) {
	if _, _, err := References(value); err != nil {
		return nil, err
	}
	projected := map[string]any{}
	for _, key := range []string{"plugins", "extends"} {
		if v, ok := value.(map[string]any)[key]; ok {
			projected[key] = v
		}
	}
	return projected, nil
}

// Like an explicitly selected lock, an explicit manifest trusts its containing
// directory, including checkout aliases and macOS /var. Resolve that directory
// once, never the file itself. Automatic project selection has already checked
// its .github directory through OpenProjectLock before choosing the manifest.
func physicalPluginManifestPath(absolute string) (string, error) {
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

// ValidateLocalPath rejects symlinks in the configuration path and its ancestors.
func ValidateLocalPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin config symlink refused: %s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func readPluginManifest(ctx context.Context, path string, connections acquisitionhttp.Connections) ([]byte, error) {
	if address.Remote(path) {
		return configsource.New(ctx, nil, connections).ReadConfig(path)
	}
	if err := ValidateLocalPath(path); err != nil {
		return nil, err
	}
	return ReadSource(ctx, path, connections)
}

// ReadSource reads a local or remote configuration source within the file-size limit.
func ReadSource(ctx context.Context, path string, connections acquisitionhttp.Connections) ([]byte, error) {
	if address.Remote(path) {
		return configsource.New(ctx, nil, connections).ReadConfig(path)
	}
	reader := files.New(ctx, nil, nil)
	file, err := reader.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, errors.New("plugin config must be a regular file of at most 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if len(data) > MaxFileBytes {
		return nil, errors.New("plugin config exceeds 4 MiB")
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Distinguish an existing empty document from an absent document.
	return append([]byte{}, data...), nil
}

// CheckUnchanged checks every inherited file as well as the edited leaf.
func (m *File) CheckUnchanged(ctx context.Context) error {
	for path, original := range m.snapshots {
		current, err := m.read(ctx, path)
		if errors.Is(err, os.ErrNotExist) && original == nil {
			continue
		}
		if err != nil {
			return err
		}
		if original == nil || !bytes.Equal(current, original) {
			return fmt.Errorf("plugin config changed during operation (%s); retry", path)
		}
	}
	return nil
}

func (m *File) read(ctx context.Context, path string) ([]byte, error) {
	read := m.readSource
	if read == nil {
		read = func(ctx context.Context, path string) ([]byte, error) {
			return readPluginManifest(ctx, path, acquisitionhttp.Connections{})
		}
	}
	return configsource.New(ctx, func(name string) ([]byte, error) {
		return read(ctx, name)
	}, acquisitionhttp.Connections{}).ReadFile(path)
}

// OriginalBytes returns a copy of the selected config's original bytes.
func (m *File) OriginalBytes() []byte {
	return slices.Clone(m.original)
}

// Save writes an explicit leaf-level list, including [] when removing the last
// plugin.
func (m *File) Save(ctx context.Context, refs []pluginref.Reference) (err error) {
	if address.Remote(m.Path) {
		return errors.New("remote config sources are read-only; select a local file to edit plugins")
	}
	data, err := EditManifest(m.Path, m.original, refs)
	if err != nil {
		return err
	}
	if len(data) > MaxFileBytes {
		return errors.New("plugin config exceeds 4 MiB")
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}
	guard := m.Path + ".writing"
	if err := os.Mkdir(guard, 0o700); err != nil {
		return fmt.Errorf("plugin config busy (%s): %w", guard, err)
	}
	defer func() { err = errors.Join(err, os.Remove(guard)) }()
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	directory, err := os.OpenRoot(guard)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	temp, err := directory.OpenFile("content", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if e := os.Remove(temp.Name()); !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}()
	if m.Exists {
		info, err := os.Stat(m.Path)
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
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), m.Path); err != nil {
		return err
	}
	m.original = data
	m.snapshots[m.Path] = data
	m.References, m.Declared, m.Exists = slices.Clone(refs), true, true
	return nil
}

// MergedDocument returns the loaded configuration after inheritance has been merged.
func (m *File) MergedDocument() any { return m.node }

// SourceContents returns copies of the source bytes used to build the merged document.
func (m *File) SourceContents() map[string][]byte {
	sources := make(map[string][]byte, len(m.snapshots))
	for path, content := range m.snapshots {
		sources[path] = bytes.Clone(content)
	}
	return sources
}

// SetContent selects the content that Save edits. Source-change checks still use the captured files.
func (m *File) SetContent(content []byte) { m.original = bytes.Clone(content) }
