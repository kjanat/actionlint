package documentparse

import (
	actionmeta "actionlint.kjanat.dev/internal/action"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

// ActionStep decodes a composite step with its own declared shell.
// Relative working-directory values resolve from github.workspace.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func ActionStep(metadata *actionmeta.Step, parser *workflowlang.Parser) *workflowlang.Step {
	pos := &workflowlang.Pos{Line: metadata.Line, Col: metadata.Column}
	opaque := &workflowlang.Step{Pos: pos, Exec: &workflowlang.ExecAction{}}
	if metadata.Node == nil || !metadata.IsMapping {
		return opaque
	}
	if metadata.Run != nil && (metadata.Shell == nil || metadata.Uses != nil) {
		return opaque
	}
	// Reuse the step decoder for scalar coercion and source mapping. Metadata
	// validation remains responsible for the composite-specific key grammar.
	node, ok := cloneActionNode(metadata.Node, make(map[*yaml.Node]bool), make(map[*yaml.Node]*yaml.Node))
	if !ok {
		return opaque
	}
	parser.Errors = nil
	step := parser.ParseStep(actionmeta.Fields(node))
	if len(parser.Errors) != 0 {
		return opaque
	}
	switch step.Exec.(type) {
	case *workflowlang.ExecRun, *workflowlang.ExecAction:
		return step
	default:
		return opaque
	}
}

// Alias expansion must not mutate the shared metadata cache. Keep original
// scalar positions so script findings still point into the metadata source.
func cloneActionNode(node *yaml.Node, active map[*yaml.Node]bool, done map[*yaml.Node]*yaml.Node) (*yaml.Node, bool) {
	node = actionmeta.Node(node)
	if node == nil || node.Kind == yaml.AliasNode || active[node] {
		return nil, false
	}
	if cloned, ok := done[node]; ok {
		return cloned, true
	}
	active[node] = true
	defer delete(active, node)
	cloned := *node
	cloned.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		resolved, ok := cloneActionNode(child, active, done)
		if !ok {
			return nil, false
		}
		cloned.Content[i] = resolved
	}
	done[node] = &cloned
	return &cloned, true
}
