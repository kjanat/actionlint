// Package calls models the declared interface of a callable (the inputs,
// outputs and secrets of actions and reusable workflows), reusable-workflow
// metadata and argument bindings.
package calls

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

// ParseMetadata reads a reusable workflow's on.workflow_call declarations and
// recovered job facts. The workflow parser is the only reader of the
// declarations. ParseMetadata fails when the source is not YAML GitHub admits,
// when no workflow_call event triggers the workflow, or when the parser reports
// a finding inside on.workflow_call: GitHub refuses to call such a workflow.
// Findings elsewhere in the workflow belong to the callee's own diagnostics.
func ParseMetadata(src []byte) (*Metadata, error) {
	doc, err := codec.ParseYAML(src)
	if err != nil {
		return nil, err
	}
	if err := checkMetadataAliases(doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		return nil, errors.New("\"on:\" is not found")
	}
	if err := workflowparser.AdmitYAMLAliases(doc); err != nil {
		return nil, err
	}
	workflow, findings, workflowCallFindings := workflowparser.ParseReusableWorkflow(src)
	if workflow == nil {
		if len(findings) == 0 {
			return nil, errors.New("workflow could not be parsed")
		}
		return nil, findingError(findings[0])
	}
	if workflow.OnPos == nil {
		return nil, errors.New("\"on:\" is not found")
	}
	event, ok := workflowlang.FindWorkflowCallEvent(workflow)
	if !ok {
		return nil, fmt.Errorf("\"workflow_call\" event trigger is not found in \"on:\" at line:%d, column:%d", workflow.OnPos.Line, workflow.OnPos.Column)
	}
	if len(workflowCallFindings) > 0 {
		return nil, findingError(workflowCallFindings[0])
	}
	return MetadataFromWorkflowCallEvent(event, workflow), nil
}

// hasDefault reports whether input supplies a default value. A YAML null
// default supplies none, so a required input stays required.
func hasDefault(input *workflowlang.WorkflowCallEventInput) bool {
	return input.Default != nil && input.Default.SourceScalar.Tag != workflowlang.YamlTagNull
}

func findingError(finding *workflowparser.SyntaxError) error {
	return fmt.Errorf("line:%d, column:%d: %s", finding.Line, finding.Column, finding.Message)
}

func checkMetadataAliases(doc *codec.Node) error {
	var aliasErr error
	workflowparser.ValidateYAMLAliases(doc, func(n *codec.Node, kind workflowparser.YamlAliasDiagnostic, message string) {
		if kind != workflowparser.YamlAliasDiagnosticUnusedAnchor && aliasErr == nil {
			aliasErr = fmt.Errorf("line:%d, column:%d: %s", n.Line, n.Column, message)
		}
	})
	return aliasErr
}

// MetadataFromWorkflowCallEvent converts parsed workflow_call declarations and
// the job facts of workflow. An omitted section stays nil; an empty one is an
// empty map. A nil workflow records no job facts.
func MetadataFromWorkflowCallEvent(event *workflowlang.WorkflowCallEvent, workflow *workflowlang.Workflow) *Metadata {
	metadata := &Metadata{}
	if event.Inputs != nil {
		metadata.Inputs = make(Inputs, len(event.Inputs))
		for _, input := range event.Inputs {
			metadata.Inputs[input.ID] = Input{
				Name:     input.Name.Value,
				Required: input.IsRequired() && !hasDefault(input),
				Type:     input.Type,
			}
		}
	}
	if event.Outputs != nil {
		metadata.Outputs = make(Outputs, len(event.Outputs))
		for id, output := range event.Outputs {
			metadata.Outputs[id] = Output{Name: output.Name.Value}
		}
	}
	if event.Secrets != nil {
		metadata.Secrets = make(Secrets, len(event.Secrets))
		for id, secret := range event.Secrets {
			metadata.Secrets[id] = Secret{Name: secret.Name.Value, Required: secret.Required != nil && secret.Required.Value}
		}
	}
	metadata.JobPermissions, metadata.JobCacheAccess = JobPermissionsAndCacheAccess(workflow)
	return metadata
}

// Metadata describes reusable-workflow inputs, outputs, secrets and job access.
// Clone detaches its maps and slices when a consumer needs its own copy.
type Metadata struct {
	Inputs  Inputs
	Outputs Outputs
	Secrets Secrets
	// JobPermissions maps a job ID of the reusable workflow to the permission scopes that job requires.
	// A job which declares no "permissions:" of its own and inherits none from the workflow has no entry,
	// and neither has a job whose declaration requires nothing.
	JobPermissions map[string]PermissionScopeLevels
	// JobCacheAccess records effective declarations and local workflow calls by job ID.
	JobCacheAccess map[string]CacheAccess
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
	var operations []string
	for step := range workflowlang.AllSteps(steps) {
		if exec, ok := step.Exec.(*workflowlang.ExecAction); ok && exec.Uses != nil {
			if name := references.CacheAction(exec.Uses.Value); name != "" {
				operations = append(operations, name)
			}
		}
	}
	if mode == nil && local == "" && len(operations) == 0 {
		return CacheAccess{}, false
	}
	return CacheAccess{Mode: cacheModeDeclaration(mode), Uses: local, SourceUses: uses, Operations: operations}, true
}

// JobPermissionsAndCacheAccess collects effective permissions and cache declarations by job ID.
// Jobs without either have no entry. A map without entries is nil.
func JobPermissionsAndCacheAccess(workflow *workflowlang.Workflow) (map[string]PermissionScopeLevels, map[string]CacheAccess) {
	if workflow == nil {
		return nil, nil
	}
	var permissionsByJob map[string]PermissionScopeLevels
	var cacheByJob map[string]CacheAccess
	permissions := ResolvePermissions(workflow.Permissions)
	for _, job := range workflow.Jobs {
		uses := ""
		if job.WorkflowCall != nil && job.WorkflowCall.Uses != nil {
			uses = job.WorkflowCall.Uses.Value
		}
		if access, ok := jobCacheAccess(workflowlang.EffectiveCacheMode(workflow.CacheMode, job.CacheMode), uses, job.Steps); ok {
			if cacheByJob == nil {
				cacheByJob = map[string]CacheAccess{}
			}
			cacheByJob[job.ID.Value] = access
		}
		resolved := permissions
		if job.Permissions != nil {
			resolved = ResolvePermissions(job.Permissions)
		}
		if resolved.Kind != PermissionsDeclared || len(resolved.Levels) == 0 {
			continue
		}
		if permissionsByJob == nil {
			permissionsByJob = map[string]PermissionScopeLevels{}
		}
		permissionsByJob[job.ID.Value] = resolved.Levels
	}
	return permissionsByJob, cacheByJob
}
