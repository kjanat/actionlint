package parsing

import (
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

// Relative working-directory values resolve from github.workspace.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func actionStep(metadata *actionmeta.CompositeStep, parser *parser.Parser) *definition.Step {
	pos := new(metadata.Position)
	opaque := &definition.Step{Pos: pos, Exec: &definition.ExecAction{}}
	if metadata.Node == nil || !metadata.IsMapping {
		return opaque
	}
	if metadata.Run != nil && (metadata.Shell == nil || metadata.Uses != nil) {
		return opaque
	}
	node, ok := cloneActionNode(metadata.Node, make(map[*codec.Node]bool), make(map[*codec.Node]*codec.Node))
	if !ok {
		return opaque
	}
	step, clean := parser.ParseStepIsolated(actionmeta.Fields(node))
	if !clean {
		return opaque
	}
	switch step.Exec.(type) {
	case *definition.ExecRun, *definition.ExecAction:
		return step
	default:
		return opaque
	}
}

// Alias expansion must not mutate the shared metadata cache. Keep original
// scalar positions so script findings still point into the metadata source.
func cloneActionNode(node *codec.Node, active map[*codec.Node]bool, done map[*codec.Node]*codec.Node) (*codec.Node, bool) {
	node = actionmeta.Node(node)
	if node == nil || node.Kind == codec.AliasNode || active[node] {
		return nil, false
	}
	if cloned, ok := done[node]; ok {
		return cloned, true
	}
	active[node] = true
	defer delete(active, node)
	cloned := *node
	cloned.Content = make([]*codec.Node, len(node.Content))
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
