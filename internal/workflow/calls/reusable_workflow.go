package calls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"actionlint.kjanat.dev/v2/internal/workflow/references"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	"actionlint.kjanat.dev/v2/source"
)

func expectedMapping(where string, n *syntax.Node) error {
	return fmt.Errorf(
		"yaml: %s must be mapping node but %s node was found at line:%d, col:%d",
		where,
		workflowparser.NodeKindName(n.Kind),
		n.Line,
		n.Column,
	)
}

// InputType is the declared type of a reusable workflow input. GitHub accepts
// only boolean, number and string; any other or omitted type is unknown.
type InputType uint8

const (
	// InputTypeAny is an unknown type; every value is assignable.
	InputTypeAny InputType = iota
	// InputTypeBoolean is "type: boolean".
	InputTypeBoolean
	// InputTypeNumber is "type: number".
	InputTypeNumber
	// InputTypeString is "type: string".
	InputTypeString
)

// ExprType returns the expression type a value passed to the input must be assignable to.
func (t InputType) ExprType() exprlang.Type {
	switch t {
	case InputTypeBoolean:
		return exprlang.BoolType()
	case InputTypeNumber:
		return exprlang.NumberType()
	case InputTypeString:
		return exprlang.StringType()
	case InputTypeAny:
	}
	return exprlang.AnyType()
}

// String returns the expression type's spelling, as used in diagnostics.
func (t InputType) String() string { return t.ExprType().String() }

// Input is an input metadata for validating local reusable workflow file.
type Input struct {
	// Name is a name of the input defined in the reusable workflow.
	Name string
	// Required is true when 'required' field of the input is set to true and no default value is set.
	Required bool
	// Type is the declared type of the input.
	Type InputType
}

// Inputs is a map from input name to reusable wokflow input metadata. The
// keys are in lower case since input names of workflow calls are case insensitive.
type Inputs map[string]Input

// Secret is a secret metadata for validating local reusable workflow file.
type Secret struct {
	// Name is a name of the secret in the reusable workflow.
	Name string
	// Required indicates whether the secret is required by its reusable workflow. When this value
	// is true, workflow calls must set this secret unless secrets are not inherited.
	Required bool `yaml:"required"`
}

// Secrets is a map from secret name to reusable wokflow secret metadata.
// The keys are in lower case since secret names of workflow calls are case insensitive.
type Secrets map[string]Secret

// Output is an output metadata for validating local reusable workflow file.
type Output struct {
	// Name is a name of the output in the reusable workflow.
	Name string
}

// Outputs is a map from output name to reusable wokflow output metadata.
// The keys are in lower case since output names of workflow calls are case insensitive.
type Outputs map[string]Output

// Metadata is metadata to validate local reusable workflows. This struct does not
// contain all metadata from YAML file. It only contains metadata which is necessary to validate
// reusable workflow files by actionlint.
//
// It is a plain value record. Cache keeps its own copy and hands every lookup
// a detached one, so callers may freely modify what they receive.
type Metadata struct {
	Inputs  Inputs  `yaml:"inputs"`
	Outputs Outputs `yaml:"outputs"`
	Secrets Secrets `yaml:"secrets"`
	// JobPermissions maps a job ID of the reusable workflow to the permission scopes that job requires.
	// A job which declares no "permissions:" of its own and inherits none from the workflow has no entry,
	// and neither has a job whose declaration requires nothing.
	JobPermissions map[string]PermissionScopeLevels `yaml:"-"`
	// JobCacheAccess records effective declarations and local workflow calls by job ID.
	JobCacheAccess map[string]CacheAccess `yaml:"-"`
}

// CacheModeDeclaration is a job's effective cache-mode declaration. The zero
// value declares none: the job inherits the calling workflow's access limit.
type CacheModeDeclaration struct {
	kind     workflowlang.CacheModeKind
	declared bool
}

// DeclaredCacheMode records an explicit, possibly invalid, cache-mode declaration.
func DeclaredCacheMode(kind workflowlang.CacheModeKind) CacheModeDeclaration {
	return CacheModeDeclaration{kind: kind, declared: true}
}

func cacheModeDeclaration(mode *workflowlang.CacheMode) CacheModeDeclaration {
	if mode == nil {
		return CacheModeDeclaration{}
	}
	return DeclaredCacheMode(mode.Kind)
}

// Kind returns the declared mode. It reports false when the job declares none.
func (d CacheModeDeclaration) Kind() (workflowlang.CacheModeKind, bool) { return d.kind, d.declared }

// Equal reports whether both declarations are the same.
func (d CacheModeDeclaration) Equal(other CacheModeDeclaration) bool { return d == other }

// CacheAccess describes a job's declared cache access and any local callee.
type CacheAccess struct {
	// Mode is the job's effective declaration, if any.
	Mode CacheModeDeclaration
	// Uses is the normalized local workflow reference, or empty for other jobs.
	Uses string
	// SourceUses preserves the local reference's source spelling for diagnostics.
	SourceUses string
	// Operations lists official cache action names used by this job's steps.
	Operations []string
}

// jobCacheAccess describes one job, or reports false when the job declares no
// cache mode, calls no local workflow and uses no cache action.
func jobCacheAccess(mode *workflowlang.CacheMode, uses string, steps []*workflowlang.Step) (CacheAccess, bool) {
	local, ok := references.LocalWorkflow(uses)
	if !ok {
		local = ""
		uses = ""
	}
	operations := collectCacheOperations(nil, steps)
	if mode == nil && local == "" && len(operations) == 0 {
		return CacheAccess{}, false
	}
	return CacheAccess{Mode: cacheModeDeclaration(mode), Uses: local, SourceUses: uses, Operations: operations}, true
}

// cloneMetadata returns a copy of m sharing no storage with it.
func cloneMetadata(m *Metadata) *Metadata {
	if m == nil {
		return nil
	}
	return &Metadata{
		Inputs:         maps.Clone(m.Inputs),
		Outputs:        maps.Clone(m.Outputs),
		Secrets:        maps.Clone(m.Secrets),
		JobPermissions: cloneJobPermissions(m.JobPermissions),
		JobCacheAccess: cloneJobCacheAccess(m.JobCacheAccess),
	}
}

func cloneJobPermissions(jobs map[string]PermissionScopeLevels) map[string]PermissionScopeLevels {
	if jobs == nil {
		return nil
	}
	out := make(map[string]PermissionScopeLevels, len(jobs))
	for id, levels := range jobs {
		out[id] = maps.Clone(levels)
	}
	return out
}

func cloneJobCacheAccess(jobs map[string]CacheAccess) map[string]CacheAccess {
	if jobs == nil {
		return nil
	}
	out := make(map[string]CacheAccess, len(jobs))
	for id, access := range jobs {
		out[id] = CacheAccess{Mode: access.Mode, Uses: access.Uses, SourceUses: access.SourceUses, Operations: slices.Clone(access.Operations)}
	}
	return out
}

func collectCacheOperations(operations []string, steps []*workflowlang.Step) []string {
	for _, step := range steps {
		switch exec := step.Exec.(type) {
		case *workflowlang.ExecAction:
			if exec.Uses != nil {
				if name := references.CacheAction(exec.Uses.Value); name != "" {
					operations = append(operations, name)
				}
			}
		case *workflowlang.ExecParallel:
			operations = collectCacheOperations(operations, exec.Steps)
		}
	}
	return operations
}

// Cache is a cache for local reusable workflow metadata files. It avoids find/read/parse
// local reusable workflow YAML files. This cache is dedicated for a single project (repository)
// indicated by 'root' field. One Cache instance needs to be created per one
// project.
type Cache struct {
	captures   *capture.Store
	ctx        context.Context
	sourceRoot string
	mu         sync.RWMutex
	root       string // empty when no repository is available
	cache      map[string]reusableWorkflowMetadataResult
	cwd        string
	dbg        io.Writer
}

type reusableWorkflowMetadataResult struct {
	metadata *Metadata
	failure  metadataError // zero when metadata was read and parsed, or deliberately ignored
	captured capture.Snapshot
}

// failureKind says which step of loading a reusable workflow failed.
type failureKind uint8

const (
	failureRead failureKind = iota + 1
	failureParse
)

// metadataError is a cached failure to read or parse a reusable workflow.
// It is an immutable value: each lookup receives its own copy, and a call
// site can name the workflow in its own spelling without changing the
// cached failure. The zero value records no failure.
type metadataError struct {
	kind  failureKind
	spec  string
	cause string
}

func readFailure(spec string, err error) metadataError {
	// A capture failure is shown by its cause, which already names the file.
	shown := err
	if captured, ok := errors.AsType[*capture.CaptureError](err); ok && errors.Unwrap(captured) != nil {
		shown = errors.Unwrap(captured)
	}
	return metadataError{kind: failureRead, spec: spec, cause: shown.Error()}
}

func parseFailure(spec string, err error) metadataError {
	return metadataError{kind: failureParse, spec: spec, cause: strings.ReplaceAll(err.Error(), "\n", " ")}
}

func (f metadataError) failed() bool { return f.kind != 0 }

// at names the workflow as the call site spelled it.
func (f metadataError) at(spec string) metadataError {
	f.spec = spec
	return f
}

func (f metadataError) Error() string {
	if f.kind == failureParse {
		return fmt.Sprintf("error while parsing reusable workflow %q: %s", f.spec, f.cause)
	}
	return fmt.Sprintf("could not read reusable workflow file for %q: %s", f.spec, f.cause)
}

func (c *Cache) debug(format string, args ...any) {
	if c.dbg == nil {
		return
	}
	format = "[Cache] " + format + "\n"
	_, _ = fmt.Fprintf(c.dbg, format, args...)
}

// Lookup returns a memoized dependency result without reading the filesystem.
// The metadata is a detached copy owned by the caller.
func (c *Cache) Lookup(key string) (*Metadata, error, bool) {
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
	return cloneMetadata(result.metadata), nil, true
}

// Store records a copy of parsed metadata for a repository-relative
// specification. A nil val records that the specification has no metadata.
func (c *Cache) Store(key string, val *Metadata) {
	c.store(key, reusableWorkflowMetadataResult{metadata: cloneMetadata(val)})
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
func (c *Cache) Seed(snapshot documentparse.SourceSnapshot) error {
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
	if snapshot.Kind() != documentparse.KindWorkflow {
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
	if metadata, err := ParseMetadata(captured.Bytes()); err != nil {
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

// FindMetadata finds/parses a reusable workflow metadata located by the 'spec' argument. When project
// is not set to 'root' field or the spec does not start with "./", this method immediately returns with nil.
//
// Read and parse errors are cached alongside metadata and returned on every lookup. Each caller
// can report the failure at its own source position, regardless of the order of concurrent checks.
//
// Calling this method is thread-safe.
func (c *Cache) FindMetadata(spec string) (*Metadata, error) {
	if c.root == "" || !strings.HasPrefix(spec, "./") || exprlang.ContainsExpression(spec) {
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
	captured, err := c.captures.Read(c.ctx, file, source.YAML)
	if err != nil {
		failure := readFailure(spec, err)
		c.store(spec, reusableWorkflowMetadataResult{failure: failure})
		return nil, failure
	}

	m, err := ParseMetadata(captured.Bytes())
	if err != nil {
		failure := parseFailure(spec, err)
		c.store(spec, reusableWorkflowMetadataResult{failure: failure})
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

// FindMetadataForCall owns the source-reference boundary for diagnostic rules.
// Lookups normalize cache keys to ./ paths and name failed workflows with the call's source spelling.
// Invalid, remote, and expression references are handled by syntax validation.
func (c *Cache) FindMetadataForCall(sourceSpec string) (*Metadata, error) {
	localSpec, ok := references.LocalWorkflow(sourceSpec)
	if !ok {
		return nil, nil
	}
	m, err := c.FindMetadata(localSpec)
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

// NewCache creates a new Cache instance for the given
// project. 'cwd' is a current working directory as an absolute file path. The 'Local' means that
// the cache instance is project-local. It is not available across multiple projects.
// An empty root, used when no project is found, yields a cache that never hits.
// Callers sharing one cache per project keep their own instance per root.
func NewCache(root, cwd string, dbg io.Writer) *Cache {
	return &Cache{
		root:  root,
		cache: map[string]reusableWorkflowMetadataResult{},
		cwd:   cwd,
		dbg:   dbg,
	}
}
