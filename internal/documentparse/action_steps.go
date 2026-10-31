package documentparse

import (
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// actionStep decodes an admitted composite step with its own declared shell.
// Relative working-directory values resolve from github.workspace.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func actionStep(metadata *actionmeta.Step, parser *workflowparser.Parser) *workflowlang.Step {
	pos := &workflowlang.Pos{Line: metadata.Line, Col: metadata.Column}
	opaque := &workflowlang.Step{Pos: pos, Exec: &workflowlang.ExecAction{}}
	if metadata.Node == nil || !metadata.IsMapping {
		return opaque
	}
	if metadata.Run != nil && (metadata.Shell == nil || metadata.Uses != nil) {
		return opaque
	}
	// Reuse the step decoder for scalar coercion and source mapping.
	node, ok := cloneActionNode(metadata.Node, make(map[*syntax.Node]bool), make(map[*syntax.Node]*syntax.Node))
	if !ok {
		return opaque
	}
	step, clean := parser.ParseStepIsolated(actionmeta.Fields(node))
	if !clean {
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
func cloneActionNode(node *syntax.Node, active map[*syntax.Node]bool, done map[*syntax.Node]*syntax.Node) (*syntax.Node, bool) {
	node = actionmeta.Node(node)
	if node == nil || node.Kind == syntax.AliasNode || active[node] {
		return nil, false
	}
	if cloned, ok := done[node]; ok {
		return cloned, true
	}
	active[node] = true
	defer delete(active, node)
	cloned := *node
	cloned.Content = make([]*syntax.Node, len(node.Content))
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
