package localactions

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

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

type metadataSource struct {
	path       string
	address    string
	captured   capture.CapturedDocument
	provenance parsing.Provenance
	files      actionmeta.ReferencedFiles
}

// ParseError reports an unparsable action manifest and is immutable once created.
type ParseError struct {
	path  string
	cause error
}

func NewParseError(path string, cause error) *ParseError {
	return &ParseError{path: path, cause: cause}
}

func (e *ParseError) Path() string { return e.path }

func (e *ParseError) Error() string {
	if e.cause == nil {
		return fmt.Sprintf("could not parse action metadata in %q", e.path)
	}
	return fmt.Sprintf("could not parse action metadata in %q: %s", filepath.Dir(e.path), codec.FormatLoadError(e.cause))
}
func (e *ParseError) Unwrap() error { return e.cause }

// ManifestCache must be configured before concurrent analysis starts.
type ManifestCache struct {
	captures          *capture.Store
	ctx               context.Context
	sourceBase        string
	sourceRoot        string
	explicitDocuments map[string]struct{}
	onDocument        func(capture.CapturedDocument, string, *actionmeta.Metadata, error)
	onManifestRequest func(string)
	reader            *files.Reader
	customReader      bool
	mu                sync.RWMutex
	root              string
	cache             map[string]metadataEntry
	sources           map[string]metadataSource
	dbg               io.Writer
}

// Options publishes each manifest once; different documents may notify concurrently.
type Options struct {
	Reader            *files.Reader
	CustomReader      bool
	Captures          *capture.Store
	Context           context.Context
	WorkingDir        string
	ExplicitDocuments map[string]struct{}
	// OnDocument completes before the publishing lookup returns. Cache hits can
	// return during notification; consumers join all lookups before reading outlines.
	OnDocument        func(capture.CapturedDocument, string, *actionmeta.Metadata, error)
	OnManifestRequest func(string)
}

// Configure installs shared input resources before any lookups start.
func (c *ManifestCache) Configure(options Options) {
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

// NewManifestCache creates a repository metadata cache bound to ctx. An empty root disables lookups.
func NewManifestCache(ctx context.Context, root string, dbg io.Writer) *ManifestCache {
	return &ManifestCache{
		ctx:     ctx,
		reader:  files.New(ctx, nil, nil),
		root:    root,
		cache:   map[string]metadataEntry{},
		sources: map[string]metadataSource{},
		dbg:     dbg,
	}
}

func newNullManifestCache(dbg io.Writer) *ManifestCache {
	return &ManifestCache{dbg: dbg}
}

func (c *ManifestCache) debug(format string, args ...any) {
	if c.dbg == nil {
		return
	}
	format = "[LocalActionsCache] " + format + "\n"
	_, _ = fmt.Fprintf(c.dbg, format, args...)
}

func (c *ManifestCache) readCache(key string) (MetadataResult, bool) {
	c.mu.RLock()
	m, ok := c.cache[key]
	c.mu.RUnlock()
	return m.result, ok
}

func (c *ManifestCache) writeCache(key string, result MetadataResult, source metadataSource) (MetadataResult, bool) {
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

func (c *ManifestCache) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	metadata, cached, _, err := c.FindMetadataDocument(spec)
	return metadata, cached, err
}

func (c *ManifestCache) FindMetadataDocument(spec string) (*actionmeta.Metadata, bool, SourceDocument, error) {
	result, cached := c.LookupMetadata(spec)
	document, _ := c.SourceDocument(result)
	return result.Metadata(), cached, document, c.reportMetadataError(result)
}

func (c *ManifestCache) SourceDocument(result MetadataResult) (SourceDocument, bool) {
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

func (c *ManifestCache) LookupMetadata(spec string) (MetadataResult, bool) {
	if local, ok := callreferences.SelfRepositoryLocal(spec); ok {
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
	document := parsing.Action(loadedSource.path, captured.Bytes())
	loadedSource.provenance = document.Provenance
	meta, parseErr := document.Metadata, document.ParseError
	loaded := MetadataResult{status: MetadataResolved, metadata: meta}
	if err := parseErr; err != nil {
		cause := NewParseError(loadedSource.path, err)
		loaded = MetadataResult{status: MetadataParseFailed, cause: cause}
	} else {
		c.debug("New metadata parsed from action %s: %s action %q", dir, meta.Runs.Using, meta.Name)
	}
	if parseErr == nil {
		facts, err := InspectReferencedFiles(c.ctx, meta, c.reader)
		if err != nil {
			return c.writeCache(spec, MetadataResult{status: MetadataReadFailed, cause: err}, loadedSource)
		}
		loadedSource.files = facts
	}
	result, cached := c.writeCache(spec, loaded, loadedSource)
	if !cached && c.onDocument != nil {
		c.onDocument(captured, loadedSource.path, meta, parseErr)
	}
	return result, cached
}

func (c *ManifestCache) hasDockerfile(dir string) (bool, error) {
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

func (c *ManifestCache) readLocalActionMetadataFile(dir string) (capture.CapturedDocument, string, error) {
	if c.captures == nil {
		return capture.CapturedDocument{}, "", errors.New("local action cache requires a capture store")
	}
	for _, name := range []string{"action.yml", "action.yaml"} {
		path := filepath.Join(dir, name)
		if c.onManifestRequest != nil {
			c.onManifestRequest(path)
		}
		captured, err := c.captures.Read(c.ctx, path, sourcetext.YAML)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return capture.CapturedDocument{}, "", capture.NewReadError(fmt.Sprintf("could not read action metadata in %q", dir), err)
		}
		return captured, name, nil
	}

	return capture.CapturedDocument{}, "", nil
}

func (c *ManifestCache) hasExplicitAction(spec string) (bool, error) {
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

type Factory struct {
	caches map[string]*ManifestCache
	dbg    io.Writer
}

// GetCache must run serially before concurrent metadata lookups start.
func (f *Factory) GetCache(ctx context.Context, root string) *ManifestCache {
	if root == "" {
		return newNullManifestCache(f.dbg)
	}
	r := root
	if c, ok := f.caches[r]; ok {
		return c
	}
	c := NewManifestCache(ctx, root, f.dbg)
	f.caches[r] = c
	return c
}

func NewFactory(dbg io.Writer) *Factory {
	return &Factory{map[string]*ManifestCache{}, dbg}
}
