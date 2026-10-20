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

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func expectedMapping(where string, n *yaml.Node) error {
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

// UnmarshalYAML implements yaml.Unmarshaler.
func (input *Input) UnmarshalYAML(n *yaml.Node) error {
	type metadata struct {
		Required bool    `yaml:"required"`
		Default  *string `yaml:"default"`
		Type     string  `yaml:"type"`
	}

	var md metadata
	if err := n.Decode(&md); err != nil {
		return err
	}

	input.Required = md.Required && md.Default == nil
	switch md.Type {
	case "boolean":
		input.Type = exprlang.BoolType{}
	case "number":
		input.Type = exprlang.NumberType{}
	case "string":
		input.Type = exprlang.StringType{}
	default:
		input.Type = exprlang.AnyType{}
	}

	return nil
}

// Inputs is a map from input name to reusable wokflow input metadata. The
// keys are in lower case since input names of workflow calls are case insensitive.
type Inputs map[string]*Input

// UnmarshalYAML implements yaml.Unmarshaler.
func (inputs *Inputs) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return expectedMapping("on.workflow_call.inputs", n)
	}

	md := make(Inputs, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]

		var m Input
		if err := v.Decode(&m); err != nil {
			return err
		}
		m.Name = k.Value
		if m.Type == nil {
			m.Type = exprlang.AnyType{} // Reach here when `v` is null node
		}

		md[strings.ToLower(k.Value)] = &m
	}

	*inputs = md
	return nil
}

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

// UnmarshalYAML implements yaml.Unmarshaler.
func (secrets *Secrets) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return expectedMapping("on.workflow_call.secrets", n)
	}

	md := make(Secrets, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]

		var s Secret
		if err := v.Decode(&s); err != nil {
			return err
		}
		s.Name = k.Value

		md[strings.ToLower(k.Value)] = &s
	}

	*secrets = md
	return nil
}

// Output is an output metadata for validating local reusable workflow file.
type Output struct {
	// Name is a name of the output in the reusable workflow.
	Name string
}

// Outputs is a map from output name to reusable wokflow output metadata.
// The keys are in lower case since output names of workflow calls are case insensitive.
type Outputs map[string]*Output

// UnmarshalYAML implements yaml.Unmarshaler.
func (outputs *Outputs) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return expectedMapping("on.workflow_call.outputs", n)
	}

	md := make(Outputs, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		md[strings.ToLower(k.Value)] = &Output{
			Name: k.Value,
		}
	}

	*outputs = md
	return nil
}

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
	reader *fsread.Reader
	mu     sync.RWMutex
	root   string // empty when no repository is available
	cache  map[string]reusableWorkflowMetadataResult
	cwd    string
	dbg    io.Writer
}

type reusableWorkflowMetadataResult struct {
	metadata *Metadata
	err      error
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
	return result.metadata, result.err, ok
}

// Store records parsed metadata or a read failure for a repository-relative specification.
func (c *Cache) Store(key string, val *Metadata, err error) {
	if c.root == "" {
		return
	}
	c.mu.Lock()
	c.cache[key] = reusableWorkflowMetadataResult{val, err}
	c.mu.Unlock()
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

	file := filepath.Join(c.root, filepath.FromSlash(spec))
	src, err := c.reader.ReadFile(file)
	if err != nil {
		err = fmt.Errorf("could not read reusable workflow file for %q: %w", spec, err)
		c.Store(spec, nil, err)
		return nil, err
	}

	m, err := ParseMetadata(src)
	if err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		err = fmt.Errorf("error while parsing reusable workflow %q: %s", spec, msg)
		c.Store(spec, nil, err)
		return nil, err
	}

	c.debug("New reusable workflow metadata at %s: %v", file, m)
	c.Store(spec, m, nil)
	return m, nil
}

// FindMetadataForCall owns the source-reference boundary for diagnostic rules.
// Cache keys and cached errors retain the normalized ./ spelling; each lookup
// renders a separate error using the source spelling without changing the cache.
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

func (c *Cache) convWorkflowPathToSpec(p string) (string, bool) {
	if c.root == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.cwd, p)
	}
	// Compare canonical identities without changing caller-owned project or
	// source spellings used by readers and diagnostics.
	p = absPath(p)
	r := absPath(c.root)
	p, err := filepath.Rel(r, p)
	if err != nil || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", false
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "./") {
		p = "./" + p
	}
	return p, true
}

// WriteWorkflowCallEvent writes reusable workflow metadata by converting from WorkflowCallEvent AST
// node. The 'wpath' parameter is a path to the workflow file of the AST, which is a relative to the
// project root directory or an absolute path.
// This method does nothing when (1) no project is set, (2) it could not convert the workflow path
// to workflow call spec, (3) some cache for the workflow is already existing.
// This method is thread safe.
func (c *Cache) WriteWorkflowCallEvent(wpath string, event *workflowlang.WorkflowCallEvent) {
	c.WriteWorkflowCallEventFromWorkflow(wpath, event, nil)
}

// WriteWorkflowCallEventFromWorkflow is like WriteWorkflowCallEvent but also records the permissions
// each job of the reusable workflow requires, taken from the workflow AST. The permission check on
// workflow calls needs them. Passing a nil 'w' is equivalent to WriteWorkflowCallEvent. The same
// do-nothing conditions and thread safety apply.
func (c *Cache) WriteWorkflowCallEventFromWorkflow(wpath string, event *workflowlang.WorkflowCallEvent, w *workflowlang.Workflow) {
	// Convert workflow path to workflow call spec
	spec, ok := c.convWorkflowPathToSpec(wpath)
	if !ok {
		return
	}
	c.debug("Workflow call spec from workflow path %s: %s", wpath, spec)

	c.mu.RLock()
	_, ok = c.cache[spec]
	c.mu.RUnlock()
	if ok {
		return
	}

	m := &Metadata{
		Inputs:  Inputs{},
		Outputs: Outputs{},
		Secrets: Secrets{},
	}

	for _, i := range event.Inputs {
		var t exprlang.Type = exprlang.AnyType{}
		switch i.Type {
		case workflowlang.WorkflowCallEventInputTypeBoolean:
			t = exprlang.BoolType{}
		case workflowlang.WorkflowCallEventInputTypeNumber:
			t = exprlang.NumberType{}
		case workflowlang.WorkflowCallEventInputTypeString:
			t = exprlang.StringType{}
		}
		m.Inputs[i.ID] = &Input{
			Type:     t,
			Required: i.Required != nil && i.Required.Value && i.Default == nil,
			Name:     i.Name.Value,
		}
	}

	for n, o := range event.Outputs {
		m.Outputs[n] = &Output{
			Name: o.Name.Value,
		}
	}

	for n, s := range event.Secrets {
		r := s.Required != nil && s.Required.Value
		m.Secrets[n] = &Secret{
			Required: r,
			Name:     s.Name.Value,
		}
	}

	if w != nil {
		wp := ResolvePermissionsAST(w.Permissions)
		for _, j := range w.Jobs {
			uses := ""
			if j.WorkflowCall != nil && j.WorkflowCall.Uses != nil {
				uses = j.WorkflowCall.Uses.Value
			}
			m.recordJobCacheAccess(j.ID.Value, workflowlang.EffectiveCacheMode(w.CacheMode, j.CacheMode), uses, j.Steps)
			p := wp
			if j.Permissions != nil {
				p = ResolvePermissionsAST(j.Permissions)
			}
			if p.Kind != PermissionsDeclared || len(p.Levels) == 0 {
				continue
			}
			if m.JobPermissions == nil {
				m.JobPermissions = map[string]PermissionScopeLevels{}
			}
			m.JobPermissions[j.ID.Value] = p.Levels
		}
	}

	c.mu.Lock()
	if _, exists := c.cache[spec]; !exists {
		c.cache[spec] = reusableWorkflowMetadataResult{metadata: m}
	}
	c.mu.Unlock()

	c.debug("Workflow call metadata from workflow %s: %v", wpath, m)
}

func ParseMetadata(src []byte) (*Metadata, error) {
	type workflow struct {
		On          yaml.Node `yaml:"on"`
		Permissions yaml.Node `yaml:"permissions"`
		CacheMode   yaml.Node `yaml:"cache-mode"`
		Jobs        yaml.Node `yaml:"jobs"`
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}

	var aliasErr error
	workflowlang.ResolveYAMLAliases(&doc, func(n *yaml.Node, d workflowlang.YamlAliasDiagnostic, m string) {
		if d != workflowlang.YamlAliasDiagnosticUnusedAnchor && aliasErr == nil {
			aliasErr = fmt.Errorf("line:%d, column:%d: %s", n.Line, n.Column, m)
		}
	})
	if aliasErr != nil {
		return nil, aliasErr
	}

	var w workflow
	if doc.Kind != 0 {
		n := &doc
		if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
			n = n.Content[0]
		}
		if n.Kind == yaml.MappingNode {
			first := *n
			first.Content = make([]*yaml.Node, 0, len(n.Content))
			seen := map[string]bool{}
			for i := 0; i+1 < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || key.Value == "" || key.Value == "<<" || workflowlang.RawYAMLTagError(key) != "" {
					continue
				}
				// The workflow parser reports duplicate keys and retains the first value.
				if seen[key.Value] {
					continue
				}
				seen[key.Value] = true
				first.Content = append(first.Content, key, n.Content[i+1])
			}
			n = &first
		}
		if err := n.Decode(&w); err != nil {
			return nil, err
		}
	}

	n := &w.On
	if n.Line == 0 && n.Column == 0 {
		return nil, errors.New("\"on:\" is not found")
	}

	var m *Metadata
	switch n.Kind {
	case yaml.MappingNode:
		// on:
		//   workflow_call:
		for i := 0; i < len(n.Content); i += 2 {
			k := strings.ToLower(n.Content[i].Value)
			if k == "workflow_call" {
				m = &Metadata{}
				if err := n.Content[i+1].Decode(m); err != nil {
					return nil, err
				}
				break
			}
		}
	case yaml.ScalarNode:
		// on: workflow_call
		if v := strings.ToLower(n.Value); v == "workflow_call" {
			m = &Metadata{}
		}
	case yaml.SequenceNode:
		// on: [workflow_call]
		for _, c := range n.Content {
			e := strings.ToLower(c.Value)
			if e == "workflow_call" {
				m = &Metadata{}
				break
			}
		}
	}

	if m == nil {
		return nil, fmt.Errorf("\"workflow_call\" event trigger is not found in \"on:\" at line:%d, column:%d", n.Line, n.Column)
	}

	wp := ResolvePermissionsYAML(&w.Permissions)
	wc := workflowlang.CacheModeFromYAML(&w.CacheMode)
	stepParser := workflowlang.NewSourceParser(src)
	if w.Jobs.Kind == yaml.MappingNode {
		seenJobs := map[string]bool{}
		for i := 0; i+1 < len(w.Jobs.Content); i += 2 {
			id, job := w.Jobs.Content[i], w.Jobs.Content[i+1]
			key := strings.ToLower(id.Value)
			if seenJobs[key] {
				continue
			}
			seenJobs[key] = true
			if job.Kind != yaml.MappingNode {
				continue
			}
			p := wp
			mode, uses := wc, ""
			stepsOnly := false
			seenKeys := map[string]bool{}
			var steps []*workflowlang.Step
			for k := 0; k+1 < len(job.Content); k += 2 {
				key := job.Content[k].Value
				// parseMapping keeps the first occurrence after reporting a duplicate.
				if seenKeys[key] {
					continue
				}
				seenKeys[key] = true
				if workflowlang.JobKeyRequiresSteps(key) {
					stepsOnly = true
				}
				switch key {
				case "steps":
					steps = stepParser.ParseSteps(job.Content[k+1])
				case "permissions":
					p = ResolvePermissionsYAML(job.Content[k+1])
				case "cache-mode":
					mode = workflowlang.CacheModeFromYAML(job.Content[k+1])
				case "uses":
					if n := job.Content[k+1]; n.Kind == yaml.ScalarNode {
						uses = n.Value
						if literal := exprlang.LiteralExpressionValue(uses); literal != nil {
							uses = *literal
						}
					}
				}
			}
			if stepsOnly {
				uses = ""
			}
			m.recordJobCacheAccess(id.Value, mode, uses, steps)
			if p.Kind != PermissionsDeclared || len(p.Levels) == 0 {
				continue
			}
			if m.JobPermissions == nil {
				m.JobPermissions = map[string]PermissionScopeLevels{}
			}
			m.JobPermissions[id.Value] = p.Levels
		}
	}

	return m, nil
}

// NewCache creates a new Cache instance for the given
// project. 'cwd' is a current working directory as an absolute file path. The 'Local' means that
// the cache instance is project-local. It is not available across multiple projects.
func NewCache(root, cwd string, dbg io.Writer) *Cache {
	return &Cache{
		reader: fsread.New(context.Background(), nil, nil),
		root:   root,
		cache:  map[string]reusableWorkflowMetadataResult{},
		cwd:    cwd,
		dbg:    dbg,
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
