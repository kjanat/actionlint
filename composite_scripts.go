package actionlint

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// Each invocation owns its rules: external callbacks must not share mutable
// diagnostic origins, and the same metadata can run in different caller contexts.
type compositeScriptRules struct {
	meta  *ActionMetadata
	rules []Rule
}

func (v *Visitor) visitActionScripts(call *Step, parents []Rule, active map[string]bool) error {
	action, ok := call.Exec.(*ExecAction)
	if !ok {
		return nil
	}
	var meta *ActionMetadata
	if action.Uses != nil && !action.Uses.ContainsExpression() {
		spec := action.Uses.Value
		if strings.HasPrefix(spec, "$/") {
			spec, _ = selfRepositoryUsesLocalSpec(spec)
		}
		// Metadata-load diagnostics belong to RuleAction. An unresolved action
		// still invalidates executable-bit assumptions below.
		meta, _, _ = v.actions.FindMetadata(spec)
	}
	if meta == nil || !strings.EqualFold(meta.Runs.Using, "composite") || active[meta.Path()] || len(active) >= 10 {
		for _, rule := range parents {
			if err := rule.VisitStep(call); err != nil {
				return err
			}
		}
		return nil
	}
	active[meta.Path()] = true
	defer delete(active, meta.Path())

	children := make([]Rule, 0, len(parents))
	for _, parent := range parents {
		children = append(children, compositeScriptRule(parent, call))
	}
	v.compositeRules = append(v.compositeRules, compositeScriptRules{meta, children})
	parser := &parser{sourceLines: splitSourceLines(meta.src)}
	for _, metadataStep := range meta.Runs.Steps {
		step := compositeScriptStep(metadataStep, parser)
		if _, action := step.Exec.(*ExecAction); action {
			validation := NewRuleAction(v.actions)
			if err := validation.VisitStep(step); err != nil {
				return err
			}
			v.compositeRules = append(v.compositeRules, compositeScriptRules{meta, []Rule{validation}})
			if err := v.visitActionScripts(step, children, active); err != nil {
				return err
			}
			continue
		}
		for _, rule := range children {
			if err := rule.VisitStep(step); err != nil {
				return err
			}
		}
	}
	for i, parent := range parents {
		if outer, ok := parent.(*RuleExecutableBit); ok {
			if inner, ok := children[i].(*RuleExecutableBit); ok {
				outer.pristine, outer.sequential = inner.pristine, inner.sequential
				outer.paths, outer.changed = inner.paths, inner.changed
				if call.If != nil {
					// A conditional checkout may never have run. Do not promote
					// one branch's filesystem assumptions to the caller.
					outer.pristine = false
				}
			}
		}
	}
	return nil
}

func compositeScriptRule(parent Rule, call *Step) Rule {
	var child Rule
	switch rule := parent.(type) {
	case *RuleShellcheck:
		scoped := newRuleShellcheck(rule.cmd)
		scoped.config, scoped.paths, scoped.rcArgs, scoped.onInput = rule.config, rule.paths, rule.rcArgs, rule.onInput
		child = scoped
	case *RulePyflakes:
		child = newRulePyflakes(rule.cmd)
	case *RuleExecutableBit:
		scoped := newRuleExecutableBit(rule.context)
		scoped.unix, scoped.sequential, scoped.pristine = rule.unix, rule.sequential, rule.pristine
		scoped.paths, scoped.changed = rule.paths, rule.changed
		scoped.jobEnv = rule.jobEnv || shellEnvironmentUnknown(call.Env)
		if call.Background != nil && (call.Background.Expression != nil || call.Background.Value) {
			scoped.sequential, scoped.pristine = false, false
		}
		child = scoped
	default:
		panic("unsupported composite script rule")
	}
	child.SetConfig(parent.Config())
	return child
}

// Composite steps require their own shell and do not inherit defaults.run.
// Their relative working-directory is based on github.workspace, not action_path.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func compositeScriptStep(metadata *ActionCompositeStep, parser *parser) *Step {
	pos := &Pos{Line: metadata.Line, Col: metadata.Column}
	opaque := &Step{Pos: pos, Exec: &ExecAction{}}
	if metadata.node == nil || !metadata.IsMapping {
		return opaque
	}
	if metadata.Run != nil && (metadata.shell == nil || metadata.Uses != nil) {
		return opaque
	}
	// Reuse the step decoder for scalar coercion and source mapping. Metadata
	// validation remains responsible for the composite-specific key grammar.
	node, ok := compositeScriptNode(metadata.node, make(map[*yaml.Node]bool))
	if !ok {
		return opaque
	}
	parser.errors = nil
	step := parser.parseStep(actionMetadataFields(node))
	if len(parser.errors) != 0 {
		return opaque
	}
	switch step.Exec.(type) {
	case *ExecRun, *ExecAction:
		return step
	default:
		return opaque
	}
}

// Alias expansion must not mutate the shared metadata cache. Keep original
// scalar positions so script findings still point into the metadata source.
func compositeScriptNode(node *yaml.Node, active map[*yaml.Node]bool) (*yaml.Node, bool) {
	node = actionSchemaNode(node)
	if node == nil || node.Kind == yaml.AliasNode || active[node] {
		return nil, false
	}
	active[node] = true
	defer delete(active, node)
	cloned := *node
	cloned.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		resolved, ok := compositeScriptNode(child, active)
		if !ok {
			return nil, false
		}
		cloned.Content[i] = resolved
	}
	return &cloned, true
}
