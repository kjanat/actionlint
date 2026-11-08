package workflows

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// Cache shares captured reusable-workflow metadata within one repository.
type Cache struct {
	remote     *Remote
	captures   *capture.Store
	ctx        context.Context
	sourceRoot string
	mu         sync.RWMutex
	root       string // empty when no repository is available
	cache      map[string]reusableWorkflowMetadataResult
	dbg        io.Writer
}

type reusableWorkflowMetadataResult struct {
	metadata *calls.Metadata
	failure  metadataError // zero when metadata was read and parsed, or deliberately ignored
	captured capture.Snapshot
}

// failureKind says which step of loading a reusable workflow failed.
type failureKind uint8

const (
	failureRead failureKind = iota + 1
	failureParse
)

// metadataError stores a failure's display text and cause. Renaming the call-site
// reference copies this record and preserves the original error chain.
// The zero value records no failure.
type metadataError struct {
	kind    failureKind
	spec    string
	message string
	cause   error
}

func readFailure(spec string, err error) metadataError {
	// A capture failure is shown by its cause, which already names the file.
	shown := err
	if captured, ok := errors.AsType[*capture.CaptureError](err); ok && errors.Unwrap(captured) != nil {
		shown = errors.Unwrap(captured)
	}
	return metadataError{kind: failureRead, spec: spec, message: shown.Error(), cause: err}
}

func parseFailure(spec string, err error) metadataError {
	return metadataError{kind: failureParse, spec: spec, message: strings.ReplaceAll(err.Error(), "\n", " "), cause: err}
}

func (f metadataError) failed() bool { return f.kind != 0 }

// at names the workflow as the call site spelled it.
func (f metadataError) at(spec string) metadataError {
	f.spec = spec
	return f
}

func (f metadataError) Error() string {
	if f.kind == failureParse {
		return fmt.Sprintf("error while parsing reusable workflow %q: %s", f.spec, f.message)
	}
	return fmt.Sprintf("could not read reusable workflow file for %q: %s", f.spec, f.message)
}

func (f metadataError) Unwrap() error { return f.cause }

func (c *Cache) debug(format string, args ...any) {
	if c.dbg == nil {
		return
	}
	format = "[Cache] " + format + "\n"
	_, _ = fmt.Fprintf(c.dbg, format, args...)
}

// Lookup returns a memoized dependency result without reading the filesystem.
// The metadata is a detached copy owned by the caller.
func (c *Cache) Lookup(key string) (*calls.Metadata, error, bool) {
	c.mu.RLock()
	result, ok := c.cache[key]
	c.mu.RUnlock()
	if !ok {
		return nil, nil, false
	}
	if result.captured.Valid() {
		if err := c.captures.VerifyHandle(result.captured); err != nil {
			return nil, err, true
		}
	}
	if result.failure.failed() {
		return nil, result.failure, true
	}
	return result.metadata.Clone(), nil, true
}

// Store records a copy of parsed metadata for a repository-relative
// specification. A nil val records that the specification has no metadata.
func (c *Cache) Store(key string, val *calls.Metadata) {
	c.store(key, reusableWorkflowMetadataResult{metadata: val.Clone()})
}

func (c *Cache) store(key string, result reusableWorkflowMetadataResult) {
	if c.root == "" {
		return
	}
	c.mu.Lock()
	c.cache[key] = result
	c.mu.Unlock()
}

// Seed indexes reusable metadata only from this result's admitted workflow.
// Mutable visitor ASTs never supply independent source bytes or declarations.
func (c *Cache) Seed(snapshot parsing.SourceSnapshot) error {
	if c == nil || c.root == "" {
		return nil
	}
	if c.captures == nil || !snapshot.Present() || c.ctx == nil {
		return errors.New("reusable workflow metadata requires a captured workflow")
	}
	captured := snapshot.Capture()
	if err := c.captures.VerifyHandle(captured); err != nil {
		return err
	}
	if snapshot.Kind() != parsing.KindWorkflow {
		return errors.New("reusable workflow metadata requires a captured workflow")
	}
	root, err := c.captures.CanonicalIdentity(c.ctx, c.sourceRoot)
	if err != nil {
		return err
	}
	spec, local := workflowPathToSpec(filepath.FromSlash(root), filepath.FromSlash(captured.Revision().Document()))
	if !local {
		return nil
	}
	if _, _, exists := c.Lookup(spec); exists {
		return nil
	}
	result := reusableWorkflowMetadataResult{captured: captured}
	if metadata, err := calls.ParseMetadata(captured.Bytes()); err != nil {
		result.failure = parseFailure(spec, err)
	} else {
		result.metadata = metadata
	}
	c.mu.Lock()
	if _, exists := c.cache[spec]; !exists {
		c.cache[spec] = result
	}
	c.mu.Unlock()
	// Metadata failures belong to caller diagnostics. A callee entry still
	// traverses its recovered workflow and every other rule.
	return nil
}

// Load reads and parses a repository-local workflow unless it is already cached.
// Non-local references and a disabled repository scope return no metadata.
// ctx cancels this caller's wait; the capture store owns backing IO.
// Concurrent callers receive detached metadata or the cached failure.
func (c *Cache) Load(ctx context.Context, spec string) (*calls.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.root == "" || !strings.HasPrefix(spec, "./") || exprsyntax.ContainsExpression(spec) {
		return nil, nil
	}

	if m, err, ok := c.Lookup(spec); ok {
		c.debug("Cache hit for %s: %v", spec, m)
		return m, err
	}

	file := filepath.Join(c.sourceRoot, filepath.FromSlash(spec))
	if c.captures == nil || c.ctx == nil {
		return nil, errors.New("reusable workflow cache requires a capture store")
	}
	captured, err := c.captures.Read(ctx, file, source.YAML)
	if err != nil {
		failure := readFailure(spec, err)
		c.store(spec, reusableWorkflowMetadataResult{failure: failure})
		return nil, failure
	}

	m, err := calls.ParseMetadata(captured.Bytes())
	if err != nil {
		failure := parseFailure(spec, err)
		c.store(spec, reusableWorkflowMetadataResult{failure: failure, captured: captured})
		return nil, failure
	}

	c.debug("New reusable workflow metadata at %s: %v", file, m)
	c.mu.Lock()
	if _, exists := c.cache[spec]; !exists {
		c.cache[spec] = reusableWorkflowMetadataResult{metadata: m, captured: captured}
	}
	c.mu.Unlock()
	m, err, _ = c.Lookup(spec)
	return m, err
}

// LoadForCall loads local metadata using a normalized ./ cache key and reports
// failures with the caller's reference spelling. Syntax checks handle non-local
// or invalid references; this loader returns no metadata for them.
func (c *Cache) LoadForCall(ctx context.Context, sourceSpec string) (*calls.Metadata, error) {
	localSpec, ok := references.LocalWorkflow(sourceSpec)
	if !ok {
		return nil, nil
	}
	m, err := c.Load(ctx, localSpec)
	if failure, ok := errors.AsType[metadataError](err); ok {
		return nil, failure.at(sourceSpec)
	}
	return m, err
}

func workflowPathToSpec(root, path string) (string, bool) {
	p, err := filepath.Rel(root, path)
	if err != nil || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", false
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "./") {
		p = "./" + p
	}
	return p, true
}

// NewCache indexes reusable workflows under root. An empty root disables local
// file reads. SetSources configures local captures; SetRemote supplies remote metadata.
func NewCache(root string, dbg io.Writer) *Cache {
	return &Cache{
		root:  root,
		cache: map[string]reusableWorkflowMetadataResult{},
		dbg:   dbg,
	}
}
