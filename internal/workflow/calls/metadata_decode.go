package calls

import (
	"errors"
	"fmt"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// ParseMetadata extracts reusable-call declarations and recovered job facts.
// Workflow validity is checked by the workflow parser and diagnostic rules.
func ParseMetadata(src []byte) (*Metadata, error) {
	doc, err := codec.Parse(src)
	if err != nil {
		return nil, err
	}
	var aliasErr error
	workflowlang.ValidateYAMLAliases(doc, func(n *syntax.Node, kind workflowlang.YamlAliasDiagnostic, message string) {
		if kind != workflowlang.YamlAliasDiagnosticUnusedAnchor && aliasErr == nil {
			aliasErr = fmt.Errorf("line:%d, column:%d: %s", n.Line, n.Column, message)
		}
	})
	if aliasErr != nil {
		return nil, aliasErr
	}
	if doc.Kind == 0 {
		return nil, errors.New("\"on:\" is not found")
	}
	session, cursor, err := codec.Open(doc)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	decoder := metadataDecoder{session: session}
	metadata, err := decoder.workflow(cursor)
	if err != nil {
		return nil, err
	}
	// The workflow parser owns permissions, cache modes and job-step interpretation.
	parsed, _ := workflowlang.Parse(src)
	metadata.recordWorkflowFacts(parsed)
	return metadata, nil
}

type metadataDecoder struct{ session *codec.Session }

func (d metadataDecoder) workflow(cursor codec.Cursor) (*Metadata, error) {
	root, err := cursor.Enter()
	if err != nil {
		return nil, err
	}
	if root.Node().Kind != syntax.MappingNode {
		type workflow struct{}
		var target workflow
		if err := root.Decode(&target); err != nil {
			return nil, err
		}
		return nil, errors.New("\"on:\" is not found")
	}
	seen := map[string]bool{}
	var metadata *Metadata
	for key, value := range root.Pairs() {
		entered, err := key.Enter()
		if err != nil {
			return nil, err
		}
		n := entered.Node()
		if n.Kind != syntax.ScalarNode || n.Value == "" || n.Value == "<<" || workflowlang.RawYAMLTagError(n) != "" || seen[n.Value] {
			continue
		}
		seen[n.Value] = true
		if n.Value == "on" {
			metadata, err = d.event(value)
			if err != nil {
				return nil, err
			}
		}
	}
	if metadata == nil {
		return nil, errors.New("\"on:\" is not found")
	}
	return metadata, nil
}

func (d metadataDecoder) event(event codec.Cursor) (*Metadata, error) {
	on, err := event.Enter()
	if err != nil {
		return nil, err
	}
	switch on.Node().Kind {
	case syntax.MappingNode:
		for key, value := range on.Pairs() {
			entered, err := key.Enter()
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(entered.Node().Value, "workflow_call") {
				out := &Metadata{}
				return out, d.call(value, out)
			}
		}
	case syntax.ScalarNode:
		if strings.EqualFold(on.Node().Value, "workflow_call") {
			return &Metadata{}, nil
		}
	case syntax.SequenceNode:
		for item := range on.Items() {
			entered, err := item.Enter()
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(entered.Node().Value, "workflow_call") {
				return &Metadata{}, nil
			}
		}
	}
	n := on.Node()
	return nil, fmt.Errorf("\"workflow_call\" event trigger is not found in \"on:\" at line:%d, column:%d", n.Line, n.Column)
}

func (d metadataDecoder) call(cursor codec.Cursor, out *Metadata) error {
	type Metadata struct {
		Inputs  codec.Selected `yaml:"inputs"`
		Outputs codec.Selected `yaml:"outputs"`
		Secrets codec.Selected `yaml:"secrets"`
	}
	var fields Metadata
	return cursor.Select(&fields, func(field *codec.Selected, value codec.Cursor) error {
		root, err := value.Enter()
		if err != nil {
			return err
		}
		if root.Node().Kind == syntax.ScalarNode && root.Node().Tag == "!!null" {
			return nil
		}
		switch field {
		case &fields.Inputs:
			return d.inputs(root, out)
		case &fields.Secrets:
			return d.secrets(root, out)
		case &fields.Outputs:
			return d.outputs(root, out)
		}
		return errors.New("unknown reusable-workflow metadata field")
	})
}

func (d metadataDecoder) inputs(root codec.Entered, out *Metadata) error {
	if root.Node().Kind != syntax.MappingNode {
		return d.session.Wrap(root.Node(), expectedMapping("on.workflow_call.inputs", root.Node()))
	}
	inputs := Inputs{}
	for key, value := range root.Pairs() {
		name, err := key.Enter()
		if err != nil {
			return err
		}
		var definition struct {
			Required bool    `yaml:"required"`
			Default  *string `yaml:"default"`
			Type     string  `yaml:"type"`
		}
		if err := value.Decode(&definition); err != nil {
			return err
		}
		input := &Input{Name: name.Node().Value, Required: definition.Required && definition.Default == nil}
		switch definition.Type {
		case "boolean":
			input.Type = exprlang.BoolType{}
		case "number":
			input.Type = exprlang.NumberType{}
		case "string":
			input.Type = exprlang.StringType{}
		default:
			input.Type = exprlang.AnyType{}
		}
		inputs[strings.ToLower(input.Name)] = input
	}
	out.Inputs = inputs
	return nil
}

func (d metadataDecoder) secrets(root codec.Entered, out *Metadata) error {
	if root.Node().Kind != syntax.MappingNode {
		return d.session.Wrap(root.Node(), expectedMapping("on.workflow_call.secrets", root.Node()))
	}
	secrets := Secrets{}
	for key, value := range root.Pairs() {
		name, err := key.Enter()
		if err != nil {
			return err
		}
		var secret Secret
		if err := value.Decode(&secret); err != nil {
			return err
		}
		secret.Name = name.Node().Value
		secrets[strings.ToLower(secret.Name)] = &secret
	}
	out.Secrets = secrets
	return nil
}

func (d metadataDecoder) outputs(root codec.Entered, out *Metadata) error {
	if root.Node().Kind != syntax.MappingNode {
		return d.session.Wrap(root.Node(), expectedMapping("on.workflow_call.outputs", root.Node()))
	}
	outputs := Outputs{}
	for key := range root.Pairs() {
		name, err := key.Enter()
		if err != nil {
			return err
		}
		outputs[strings.ToLower(name.Node().Value)] = &Output{Name: name.Node().Value}
	}
	out.Outputs = outputs
	return nil
}

func (m *Metadata) recordWorkflowFacts(w *workflowlang.Workflow) {
	if w == nil {
		return
	}
	permissions := ResolvePermissionsAST(w.Permissions)
	for _, job := range w.Jobs {
		uses := ""
		if job.WorkflowCall != nil && job.WorkflowCall.Uses != nil {
			uses = job.WorkflowCall.Uses.Value
		}
		m.recordJobCacheAccess(job.ID.Value, workflowlang.EffectiveCacheMode(w.CacheMode, job.CacheMode), uses, job.Steps)
		resolved := permissions
		if job.Permissions != nil {
			resolved = ResolvePermissionsAST(job.Permissions)
		}
		if resolved.Kind != PermissionsDeclared || len(resolved.Levels) == 0 {
			continue
		}
		if m.JobPermissions == nil {
			m.JobPermissions = map[string]PermissionScopeLevels{}
		}
		m.JobPermissions[job.ID.Value] = resolved.Levels
	}
}
