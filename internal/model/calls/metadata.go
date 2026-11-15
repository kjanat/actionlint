// Package calls models reusable-workflow metadata and argument bindings.
package calls

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/syntax"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
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

// Input describes an input declared by a local reusable workflow.
type Input struct {
	// Name is a name of the input defined in the reusable workflow.
	Name string
	// Required is true when 'required' field of the input is set to true and no default value is set.
	Required bool
	// Type is the declared type of the input. GitHub accepts only boolean,
	// number and string; any other or omitted type is
	// WorkflowCallEventInputTypeInvalid and accepts every value.
	Type workflowlang.WorkflowCallEventInputType
}

// Inputs maps lowercase input names to reusable workflow input metadata.
// Workflow call input names are case insensitive.
type Inputs map[string]Input

// Secret describes a secret declared by a local reusable workflow.
type Secret struct {
	// Name is a name of the secret in the reusable workflow.
	Name string
	// Required indicates whether the secret is required by its reusable workflow. When this value
	// is true, workflow calls must set this secret unless they inherit secrets.
	Required bool `yaml:"required"`
}

// Secrets maps lowercase secret names to reusable workflow secret metadata.
// Workflow call secret names are case insensitive.
type Secrets map[string]Secret

// Output describes an output declared by a local reusable workflow.
type Output struct {
	// Name is a name of the output in the reusable workflow.
	Name string
}

// Outputs maps lowercase output names to reusable workflow output metadata.
// Workflow call output names are case insensitive.
type Outputs map[string]Output

// Metadata describes reusable-workflow inputs, outputs, secrets and job access.
// Clone detaches its maps and slices when a consumer needs its own copy.
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

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/model/calls -type Metadata -output internal/model/calls/owned_copy.gen.go

// Clone returns a copy that shares no map or slice storage with m.
func (m *Metadata) Clone() *Metadata {
	if m == nil {
		return nil
	}
	result := copyOwnedMetadata(*m)
	return &result
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
