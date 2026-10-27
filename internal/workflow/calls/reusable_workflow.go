package calls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
		workflowlang.NodeKindName(n.Kind),
		n.Line,
		n.Column,
	)
}

// Input is an input metadata for validating local reusable workflow file.
type Input struct {
	// Name is a name of the input defined in the reusable workflow.
	Name string
	// Required is true when 'required' field of the input is set to true and no default value is set.
	Required bool
	// Type is a type of the input. When the input type is unknown, 'any' type is set.
	Type exprlang.Type
}

// Inputs is a map from input name to reusable wokflow input metadata. The
// keys are in lower case since input names of workflow calls are case insensitive.
type Inputs map[string]*Input

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
type Secrets map[string]*Secret

// Output is an output metadata for validating local reusable workflow file.
type Output struct {
	// Name is a name of the output in the reusable workflow.
	Name string
}

// Outputs is a map from output name to reusable wokflow output metadata.
// The keys are in lower case since output names of workflow calls are case insensitive.
type Outputs map[string]*Output

// Metadata is metadata to validate local reusable workflows. This struct does not
// contain all metadata from YAML file. It only contains metadata which is necessary to validate
// reusable workflow files by actionlint.
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

// CacheAccess describes a job's declared cache access and any local callee.
type CacheAccess struct {
	// Mode is nil when the job inherits the calling workflow's access limit.
	Mode *workflowlang.CacheMode
	// Uses is the normalized local workflow reference, or empty for other jobs.
	Uses string
	// SourceUses preserves the local reference's source spelling for diagnostics.
	SourceUses string
	// Operations lists official cache action names used by this job's steps.
	Operations []string
}

func (m *Metadata) recordJobCacheAccess(id string, mode *workflowlang.CacheMode, uses string, steps []*workflowlang.Step) {
	local, ok := references.LocalWorkflow(uses)
	if !ok {
		local = ""
		uses = ""
	}
	operations := collectCacheOperations(nil, steps)
	if mode == nil && local == "" && len(operations) == 0 {
		return
	}
	if m.JobCacheAccess == nil {
		m.JobCacheAccess = map[string]CacheAccess{}
	}
	m.JobCacheAccess[id] = CacheAccess{Mode: mode, Uses: local, SourceUses: uses, Operations: operations}
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
	err      error
	captured capture.Snapshot
}

func (c *Cache) debug(format string, args ...any) {
	if c.dbg == nil {
		return
	}
	format = "[Cache] " + format + "\n"
	_, _ = fmt.Fprintf(c.dbg, format, args...)
}

// Lookup returns a memoized dependency result without reading the filesystem.
func (c *Cache) Lookup(key string) (*Metadata, error, bool) {
	c.mu.RLock()
	result, ok := c.cache[key]
	c.mu.RUnlock()
	if ok && result.captured.Valid() {
		if err := c.captures.VerifyHandle(result.captured); err != nil {
			return nil, err, true
		}
	}
	return result.metadata, result.err, ok
}

// Store records parsed metadata or a read failure for a repository-relative specification.
func (c *Cache) Store(key string, val *Metadata, err error) {
	if c.root == "" {
		return
	}
	c.mu.Lock()
	c.cache[key] = reusableWorkflowMetadataResult{metadata: val, err: err}
	c.mu.Unlock()
}

// Seed indexes reusable metadata only from this result's admitted workflow.
// Mutable visitor ASTs never supply independent source bytes or declarations.
func (c *Cache) Seed(snapshot *documentparse.SourceSnapshot) error {
	if c == nil || c.root == "" {
		return nil
	}
	if c.captures == nil || snapshot == nil || c.ctx == nil {
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
	metadata, parseErr := ParseMetadata(captured.Bytes())
	if parseErr != nil {
		parseErr = reusableMetadataParseError(spec, parseErr)
	}
	c.mu.Lock()
	if _, exists := c.cache[spec]; !exists {
		c.cache[spec] = reusableWorkflowMetadataResult{metadata: metadata, err: parseErr, captured: captured}
	}
	c.mu.Unlock()
	// Metadata failures belong to caller diagnostics. A callee entry still
	// traverses its recovered workflow and every other rule.
	return nil
}

func reusableMetadataParseError(spec string, cause error) error {
	return fmt.Errorf("error while parsing reusable workflow %q: %s", spec, strings.ReplaceAll(cause.Error(), "\n", " "))
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
		err = fmt.Errorf("could not read reusable workflow file for %q: %w", spec, err)
		c.Store(spec, nil, err)
		return nil, err
	}

	m, err := ParseMetadata(captured.Bytes())
	if err != nil {
		err = reusableMetadataParseError(spec, err)
		c.Store(spec, nil, err)
		return nil, err
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
// Lookups normalize cache keys to ./ paths and format errors with the call's source spelling.
// Invalid, remote, and expression references are handled by syntax validation.
func (c *Cache) FindMetadataForCall(sourceSpec string) (*Metadata, error) {
	localSpec, ok := references.LocalWorkflow(sourceSpec)
	if !ok {
		return nil, nil
	}
	m, err := c.FindMetadata(localSpec)
	if err != nil {
		return m, errors.New(strings.Replace(err.Error(), strconv.Quote(localSpec), strconv.Quote(sourceSpec), 1))
	}
	return m, nil
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
func NewCache(root, cwd string, dbg io.Writer) *Cache {
	return &Cache{
		root:  root,
		cache: map[string]reusableWorkflowMetadataResult{},
		cwd:   cwd,
		dbg:   dbg,
	}
}

func newNullLocalReusableWorkflowCache(dbg io.Writer) *Cache {
	// Null cache. Cache never hits. It is used when project is not found
	return &Cache{dbg: dbg}
}

// CacheFactory is a factory object to create a Cache
// instance per project.
type CacheFactory struct {
	caches map[string]*Cache
	cwd    string
	dbg    io.Writer
}

// NewCacheFactory creates a new CacheFactory instance.
func NewCacheFactory(cwd string, dbg io.Writer) *CacheFactory {
	return &CacheFactory{map[string]*Cache{}, cwd, dbg}
}

// GetCache returns a new or existing Cache instance per project. When a instance
// was already created for the project, this method returns the existing instance. Otherwise it creates
// a new instance and returns it.
func (f *CacheFactory) GetCache(root string) *Cache {
	if root == "" {
		return newNullLocalReusableWorkflowCache(f.dbg)
	}
	r := root
	if c, ok := f.caches[r]; ok {
		return c
	}
	c := NewCache(root, f.cwd, f.dbg)
	f.caches[r] = c
	return c
}
