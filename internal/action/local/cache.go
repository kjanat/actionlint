package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/source"
)

type metadataSource struct {
	path     string
	address  string
	captured capture.Snapshot
	document documentparse.Document
}

type ParseError struct {
	Path      string
	Cause     error
	directory string
}

func (e *ParseError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("could not parse action metadata in %q", e.Path)
	}
	if e.directory == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("could not parse action metadata in %q: %s", e.directory, codec.FormatLoadError(e.Cause))
}
func (e *ParseError) Unwrap() error { return e.Cause }

// Cache shares parsed manifests and raw source snapshots for one repository.
// Configure it before concurrent analysis; callers use separate Resolver instances.
type Cache struct {
	captures          *capture.Store
	ctx               context.Context
	sourceBase        string
	sourceRoot        string
	explicitDocuments map[string]struct{}
	onDocument        func(capture.Snapshot, string, *actionmeta.Metadata, error)
	onManifestRequest func(string)
	reader            *fsread.Reader
	customReader      bool
	mu                sync.RWMutex
	root              string
	cache             map[string]metadataEntry
	sources           map[string]metadataSource
	dbg               io.Writer
}

// Options provides source snapshots, a recording reader and a document observer.
// The observer receives syntax values and errors without a reporting-model dependency.
// Published manifests are observed once; different documents may notify concurrently.
type Options struct {
	Reader            *fsread.Reader
	CustomReader      bool
	Captures          *capture.Store
	Context           context.Context
	WorkingDir        string
	ExplicitDocuments map[string]struct{}
	// OnDocument completes before the publishing lookup returns. Cache hits can
	// return during notification; consumers join all lookups before reading outlines.
	OnDocument        func(capture.Snapshot, string, *actionmeta.Metadata, error)
	OnManifestRequest func(string)
}

// Configure installs shared input resources before any lookups start.
func (c *Cache) Configure(options Options) {
	if options.Reader != nil {
		c.reader = options.Reader
	}
	c.customReader = options.CustomReader
	if options.Captures != nil {
		c.captures = options.Captures
	}
	if options.Context != nil {
		c.ctx = options.Context
	}
	if options.WorkingDir != "" {
		c.sourceBase = options.WorkingDir
	}
	c.sourceRoot = c.root
	if c.sourceRoot != "" && !filepath.IsAbs(c.sourceRoot) && c.sourceBase != "" {
		c.sourceRoot = filepath.Join(c.sourceBase, c.sourceRoot)
	}
	c.explicitDocuments = maps.Clone(options.ExplicitDocuments)
	c.onDocument = options.OnDocument
	c.onManifestRequest = options.OnManifestRequest
}

// NewCache creates a repository metadata cache. An empty root disables lookups.
func NewCache(root string, dbg io.Writer) *Cache {
	return &Cache{
		ctx:     context.Background(),
		reader:  fsread.New(context.Background(), nil, nil),
		root:    root,
		cache:   map[string]metadataEntry{},
		sources: map[string]metadataSource{},
		dbg:     dbg,
	}
}

func newNullCache(dbg io.Writer) *Cache {
	// Null cache. Cache never hits. It is used when project is not found
	return &Cache{dbg: dbg}
}

func (c *Cache) debug(format string, args ...any) {
	if c.dbg == nil {
		return
	}
	format = "[LocalActionsCache] " + format + "\n"
	_, _ = fmt.Fprintf(c.dbg, format, args...)
}

func (c *Cache) readCache(key string) (MetadataResult, bool) {
	c.mu.RLock()
	m, ok := c.cache[key]
	c.mu.RUnlock()
	return m.result, ok
}

func (c *Cache) writeCache(key string, result MetadataResult, source metadataSource) (MetadataResult, bool) {
	if errors.Is(result.cause, context.Canceled) || errors.Is(result.cause, context.DeadlineExceeded) {
		return result, false
	}
	result.cacheSpec = key
	c.mu.Lock()
	if previous, exists := c.cache[key]; exists {
		c.mu.Unlock()
		return previous.result, true
	}
	c.cache[key] = metadataEntry{result: result}
	if source.path != "" {
		c.sources[key] = source
	}
	c.mu.Unlock()
	return result, false
}

// FindMetadata reports missing manifests at each reference and other cached failures once.
func (c *Cache) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	metadata, cached, _, err := c.FindMetadataDocument(spec)
	return metadata, cached, err
}

// FindMetadataDocument returns metadata and its provenance from the same lookup,
// including the captured source of a failed parse. It performs no second read.
func (c *Cache) FindMetadataDocument(spec string) (*actionmeta.Metadata, bool, SourceDocument, error) {
	result, cached := c.LookupMetadata(spec)
	document, _ := c.SourceDocument(result)
	return result.Metadata(), cached, document, c.reportMetadataError(result)
}

// SourceDocument projects the captured parse record associated with a lookup.
func (c *Cache) SourceDocument(result MetadataResult) (SourceDocument, bool) {
	if c == nil || result.cacheSpec == "" {
		return SourceDocument{}, false
	}
	c.mu.RLock()
	source, exists := c.sources[result.cacheSpec]
	c.mu.RUnlock()
	if !exists {
		return SourceDocument{}, false
	}
	return SourceDocument(source), true
}

// LookupMetadata preserves failure causes without consuming their diagnostics.
func (c *Cache) LookupMetadata(spec string) (MetadataResult, bool) {
	if local, ok := references.SelfRepositoryLocal(spec); ok {
		spec = local
	}
	if c == nil || c.root == "" {
		return MetadataResult{status: MetadataLookupDisabled}, false
	}
	if !strings.HasPrefix(spec, "./") {
		return MetadataResult{status: MetadataNonLocal}, false
	}

	if m, ok := c.readCache(spec); ok {
		c.debug("Cache hit for %s: %v", spec, m.Metadata())
		return m, true
	}

	dir := filepath.Join(c.root, filepath.FromSlash(spec))
	address := filepath.Join(c.sourceRoot, filepath.FromSlash(spec))
	captured, f, err := c.readLocalActionMetadataFile(address)
	if err != nil {
		return c.writeCache(spec, MetadataResult{status: MetadataReadFailed, cause: err}, metadataSource{})
	}
	if f == "" {
		docker, err := c.hasDockerfile(address)
		if err != nil {
			return c.writeCache(spec, MetadataResult{status: MetadataReadFailed, cause: err}, metadataSource{})
		}
		if docker {
			// Dockerfile-only actions execute without declaring inputs or outputs.
			return c.writeCache(spec, MetadataResult{status: MetadataDockerfileOnly}, metadataSource{})
		}
		c.debug("No action metadata found in %s", dir)
		return c.writeCache(spec, MetadataResult{status: MetadataMissingManifest, cause: missingLocalActionMetadata(spec)}, metadataSource{})
	}
	loadedSource := metadataSource{path: filepath.Join(dir, f), address: filepath.Join(address, f), captured: captured}
	document := documentparse.Action(loadedSource.path, loadedSource.path, captured.Bytes())
	loadedSource.document = document
	meta, parseErr := document.Metadata, document.ActionParseError()
	loaded := MetadataResult{status: MetadataResolved, metadata: meta}
	if err := parseErr; err != nil {
		cause := &ParseError{Path: filepath.Join(dir, f), Cause: err, directory: dir}
		loaded = MetadataResult{status: MetadataParseFailed, cause: cause}
	} else {
		c.debug("New metadata parsed from action %s: %s action %q", dir, meta.Runs.Using, meta.Name)
	}
	result, cached := c.writeCache(spec, loaded, loadedSource)
	if !cached && c.onDocument != nil {
		c.onDocument(captured, loadedSource.path, meta, parseErr)
	}
	return result, cached
}

func (c *Cache) hasDockerfile(dir string) (bool, error) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		file := filepath.Join(dir, name)
		if !c.customReader {
			if info, err := os.Stat(file); err == nil && info.IsDir() {
				continue
			}
		}
		// Record the dependency without presenting Dockerfile bytes as YAML metadata.
		_, err := c.reader.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("could not read local action Dockerfile %q: %w", file, err)
		}
		return true, nil
	}
	return false, nil
}

func (c *Cache) readLocalActionMetadataFile(dir string) (capture.Snapshot, string, error) {
	if c.captures == nil {
		return capture.Snapshot{}, "", errors.New("local action cache requires a capture store")
	}
	for _, name := range []string{"action.yml", "action.yaml"} {
		path := filepath.Join(dir, name)
		if c.onManifestRequest != nil {
			c.onManifestRequest(path)
		}
		captured, err := c.captures.Read(c.ctx, path, source.YAML)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return capture.Snapshot{}, "", &capture.ReadFailure{Context: fmt.Sprintf("could not read action metadata in %q", dir), Err: err}
		}
		return captured, name, nil
	}

	return capture.Snapshot{}, "", nil
}

func (c *Cache) hasExplicitAction(spec string) (bool, error) {
	if c == nil || c.root == "" || len(c.explicitDocuments) == 0 {
		return false, nil
	}
	if c.captures == nil {
		return false, errors.New("explicit actions require a capture store")
	}
	dir := filepath.Join(c.sourceRoot, filepath.FromSlash(spec))
	for _, name := range []string{"action.yml", "action.yaml"} {
		address := filepath.Join(dir, name)
		document, err := c.captures.Canonical(c.ctx, address)
		if err != nil {
			return false, err
		}
		if _, ok := c.explicitDocuments[document]; ok {
			return true, nil
		}
	}
	return false, nil
}

// Factory shares repository metadata caches across one analysis invocation.
type Factory struct {
	caches map[string]*Cache
	dbg    io.Writer
}

// GetCache returns the shared cache for a repository root, or a disabled cache for an empty root.
// Create caches serially before starting concurrent metadata lookups.
func (f *Factory) GetCache(root string) *Cache {
	if root == "" {
		return newNullCache(f.dbg)
	}
	r := root
	if c, ok := f.caches[r]; ok {
		return c
	}
	c := NewCache(root, f.dbg)
	f.caches[r] = c
	return c
}

// NewFactory creates a cache factory for one analysis invocation.
func NewFactory(dbg io.Writer) *Factory {
	return &Factory{map[string]*Cache{}, dbg}
}
