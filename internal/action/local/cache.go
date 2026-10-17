package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	actionmeta "actionlint.kjanat.dev/internal/action"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflow/references"
	"actionlint.kjanat.dev/internal/yaml"
)

type metadataSource struct {
	path    string
	content []byte
}

type ParseError struct {
	Path  string
	Cause error
}

func (e *ParseError) Error() string { return e.Cause.Error() }
func (e *ParseError) Unwrap() error { return e.Cause }

// Cache shares parsed manifests and raw source snapshots for one repository.
// Configure it before concurrent analysis; callers use separate Resolver instances.
type Cache struct {
	explicitSources map[string][]byte
	onDocument      func(string, *actionmeta.Metadata, error)
	reader          *fsread.Reader
	customReader    bool
	mu              sync.RWMutex
	root            string
	cache           map[string]*actionmeta.Metadata
	missing         sync.Map
	sources         map[string]metadataSource
	dbg             io.Writer
}

// Options provides source snapshots, a recording reader and a document observer.
// The observer receives syntax values and errors without a reporting-model dependency.
type Options struct {
	Reader       *fsread.Reader
	CustomReader bool
	Sources      map[string][]byte
	OnDocument   func(string, *actionmeta.Metadata, error)
}

// Configure installs shared input resources before any lookups start.
func (c *Cache) Configure(options Options) {
	if options.Reader != nil {
		c.reader = options.Reader
	}
	c.customReader = options.CustomReader
	c.explicitSources = options.Sources
	c.onDocument = options.OnDocument
}

// NewCache creates a repository metadata cache. An empty root disables lookups.
func NewCache(root string, dbg io.Writer) *Cache {
	return &Cache{
		reader:  fsread.New(context.Background(), nil, nil),
		root:    root,
		cache:   map[string]*actionmeta.Metadata{},
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

func (c *Cache) readCache(key string) (*actionmeta.Metadata, bool) {
	c.mu.RLock()
	m, ok := c.cache[key]
	c.mu.RUnlock()
	return m, ok
}

func (c *Cache) writeCache(key string, val *actionmeta.Metadata) {
	c.mu.Lock()
	c.cache[key] = val
	c.mu.Unlock()
}

// FindMetadata finds metadata for a local action, specified with "./" or "$/".
// The first return value can be nil even if error did not occur.
// Missing manifests are cached and reported at every reference. Other metadata errors
// are reported on the first lookup; later lookups return the cached nil result.
// Calling this method is thread-safe.
func (c *Cache) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	if local, ok := references.SelfRepositoryLocal(spec); ok {
		spec = local
	}
	if c == nil || c.root == "" || !strings.HasPrefix(spec, "./") {
		return nil, false, nil
	}

	if m, ok := c.readCache(spec); ok {
		c.debug("Cache hit for %s: %v", spec, m)
		if _, missing := c.missing.Load(spec); missing {
			return nil, true, missingLocalActionMetadata(spec)
		}
		return m, true, nil
	}

	dir := filepath.Join(c.root, filepath.FromSlash(spec))
	if c.hasExplicitAction(spec) {
		if absolute, err := filepath.Abs(dir); err == nil {
			dir = absolute
		}
	}
	b, f, err := c.readLocalActionMetadataFile(dir)
	if err != nil {
		c.writeCache(spec, nil)
		return nil, false, err
	}
	if f == "" {
		c.debug("No action metadata found in %s", dir)
		c.missing.Store(spec, true)
		c.writeCache(spec, nil)
		return nil, false, missingLocalActionMetadata(spec)
	}
	c.mu.Lock()
	c.sources[spec] = metadataSource{path: filepath.Join(dir, f), content: b}
	c.mu.Unlock()

	meta, parseErr := actionmeta.Parse(filepath.Join(dir, f), b)
	if c.onDocument != nil {
		c.onDocument(filepath.Join(dir, f), meta, parseErr)
	}
	if err := parseErr; err != nil {
		c.writeCache(spec, nil) // Remember action was invalid

		// Unwrap load errors when a single error occurs to simplify the error message
		var m string
		if es, ok := errors.AsType[*yaml.DecodeError](err); ok {
			if len(es.Errors) == 1 {
				e := es.Errors[0]
				if e.Mark.Line > 0 {
					m = fmt.Sprintf("line %d: %s", e.Mark.Line, e.Message)
				} else {
					m = e.Message
				}
			} else {
				m = strings.ReplaceAll(es.Error(), "\n", "")
			}
		} else {
			m = err.Error()
		}

		return nil, false, &ParseError{Path: filepath.Join(dir, f), Cause: fmt.Errorf("could not parse action metadata in %q: %s", dir, m)}
	}
	c.debug("New metadata parsed from action %s: %s action %q", dir, meta.Runs.Using, meta.Name)
	c.writeCache(spec, meta)
	return meta, false, nil
}

func (c *Cache) readLocalActionMetadataFile(dir string) ([]byte, string, error) {
	for _, name := range []string{"action.yaml", "action.yml"} {
		path := filepath.Join(dir, name)
		if content, ok := c.explicitSources[fsread.PhysicalPath(path)]; ok {
			c.reader.Record(path)
			return content, name, nil
		}
		b, err := c.reader.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("could not read action metadata in %q: %w", dir, err)
		}
		return b, name, nil
	}

	return nil, "", nil
}

func (c *Cache) hasExplicitAction(spec string) bool {
	if c == nil || c.root == "" {
		return false
	}
	dir := filepath.Join(c.root, filepath.FromSlash(spec))
	for _, name := range []string{"action.yaml", "action.yml"} {
		if _, ok := c.explicitSources[fsread.PhysicalPath(filepath.Join(dir, name))]; ok {
			return true
		}
	}
	return false
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
