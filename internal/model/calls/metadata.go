// Package calls models callable interfaces, reusable-workflow metadata and argument bindings.
package calls

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

// ParseMetadata fails when the parser reports a finding inside on.workflow_call, and GitHub refuses to call such a workflow.
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
	if err := parser.AdmitYAMLAliases(doc); err != nil {
		return nil, err
	}
	workflow, findings, workflowCallFindings := parser.ParseReusableWorkflow(src)
	if workflow == nil {
		if len(findings) == 0 {
			return nil, errors.New("workflow could not be parsed")
		}
		return nil, findingError(findings[0])
	}
	if workflow.OnPos == nil {
		return nil, errors.New("\"on:\" is not found")
	}
	event, ok := definition.FindWorkflowCallEvent(workflow)
	if !ok {
		return nil, fmt.Errorf("\"workflow_call\" event trigger is not found in \"on:\" at line:%d, column:%d", workflow.OnPos.Line, workflow.OnPos.Column)
	}
	if len(workflowCallFindings) > 0 {
		return nil, findingError(workflowCallFindings[0])
	}
	return MetadataFromWorkflowCallEvent(event, workflow), nil
}

// A YAML null default supplies none, so a required input stays required.
func hasDefault(input *definition.WorkflowCallEventInput) bool {
	return input.Default != nil && input.Default.SourceScalar.Tag != definition.YamlTagNull
}

func findingError(finding *parser.SyntaxError) error {
	return fmt.Errorf("line:%d, column:%d: %s", finding.Position.Line, finding.Position.Column, finding.Message)
}

func checkMetadataAliases(doc *codec.Node) error {
	var aliasErr error
	parser.ValidateYAMLAliases(doc, func(n *codec.Node, kind parser.YamlAliasDiagnostic, message string) {
		if kind != parser.YamlAliasDiagnosticUnusedAnchor && aliasErr == nil {
			aliasErr = fmt.Errorf("line:%d, column:%d: %s", n.Line, n.Column, message)
		}
	})
	return aliasErr
}

// MetadataFromWorkflowCallEvent leaves an omitted section nil and makes an empty one an empty map.
func MetadataFromWorkflowCallEvent(event *definition.WorkflowCallEvent, workflow *definition.Workflow) *Metadata {
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

type Metadata struct {
	Inputs  Inputs
	Outputs Outputs
	Secrets Secrets
	// JobPermissions has no entry for a job that declares and inherits no permissions or requires nothing.
	JobPermissions map[string]PermissionScopeLevels
	JobCacheAccess map[string]CacheAccess
}

// CacheModeDeclaration at its zero value inherits the calling workflow's access limit.
type CacheModeDeclaration struct {
	kind     definition.CacheModeKind
	declared bool
}

func DeclaredCacheMode(kind definition.CacheModeKind) CacheModeDeclaration {
	return CacheModeDeclaration{kind: kind, declared: true}
}

func cacheModeDeclaration(mode *definition.CacheMode) CacheModeDeclaration {
	if mode == nil {
		return CacheModeDeclaration{}
	}
	return DeclaredCacheMode(mode.Kind)
}

func (d CacheModeDeclaration) Kind() (definition.CacheModeKind, bool) { return d.kind, d.declared }

func (d CacheModeDeclaration) Equal(other CacheModeDeclaration) bool { return d == other }

type CacheAccess struct {
	Mode       CacheModeDeclaration
	Uses       string
	SourceUses string
	Operations []string
}

func jobCacheAccess(mode *definition.CacheMode, uses string, steps []*definition.Step) (CacheAccess, bool) {
	local, ok := callreferences.LocalWorkflow(uses)
	if !ok {
		local = ""
		uses = ""
	}
	var operations []string
	for step := range definition.AllSteps(steps) {
		if exec, ok := step.Exec.(*definition.ExecAction); ok && exec.Uses != nil {
			if name := callreferences.CacheAction(exec.Uses.Value); name != "" {
				operations = append(operations, name)
			}
		}
	}
	if mode == nil && local == "" && len(operations) == 0 {
		return CacheAccess{}, false
	}
	return CacheAccess{Mode: cacheModeDeclaration(mode), Uses: local, SourceUses: uses, Operations: operations}, true
}

// JobPermissionsAndCacheAccess returns nil for a map without entries.
func JobPermissionsAndCacheAccess(workflow *definition.Workflow) (map[string]PermissionScopeLevels, map[string]CacheAccess) {
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
		if access, ok := jobCacheAccess(definition.EffectiveCacheMode(workflow.CacheMode, job.CacheMode), uses, job.Steps); ok {
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
