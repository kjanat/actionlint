package actionlint

import (
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Each invocation owns its rules: external callbacks must not share mutable
// diagnostic origins, and the same metadata can run in different caller contexts.
type compositeScriptRules struct {
	meta  *ActionMetadata
	rules []Rule
}

func (analysis *compositeAnalyzer) visitActionScripts(call *Step, parents []Rule, active map[string]bool) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	action, ok := call.Exec.(*ExecAction)
	if !ok {
		return nil
	}
	var meta *ActionMetadata
	if action.Uses != nil && !action.Uses.ContainsExpression() {
		spec := action.Uses.Value
		// Metadata-load diagnostics belong to RuleAction. An unresolved action
		// still invalidates executable-bit assumptions below.
		meta, _, _ = analysis.actions.FindMetadata(spec)
	}
	if meta == nil || !strings.EqualFold(meta.Runs.Using, "composite") || active[meta.Path()] || len(active) >= 10 {
		for _, rule := range parents {
			compositeCheckoutPaths(rule, analysis.actions)
			if err := rule.VisitStep(call); err != nil {
				return err
			}
		}
		return nil
	}
	active[meta.Path()] = true
	defer delete(active, meta.Path())
	checkout := analysis.actions.checkoutState()
	defer func() {
		enabled, known := invocationCondition(call.If)
		if known && !enabled {
			analysis.actions.restoreCheckout(checkout)
		} else if (!known || boolMayBeTrue(call.ContinueOnError) || boolMayBeTrue(call.Background)) && checkout != analysis.actions.checkoutState() {
			analysis.actions.setCheckout(runDirectory{kind: directoryUnknown})
		}
	}()

	children := make([]Rule, 0, len(parents))
	for _, parent := range parents {
		child := compositeScriptRule(parent, call, filepath.Dir(meta.Path()), analysis.actions)
		if shellcheck, ok := child.(*RuleShellcheck); ok {
			if err := shellcheck.prepareConfigPath(); err != nil {
				return err
			}
		}
		children = append(children, child)
	}
	analysis.rules = append(analysis.rules, compositeScriptRules{meta, children})
	parser := &parser{sourceLines: splitSourceLines(meta.src)}
	for _, metadataStep := range meta.Runs.Steps {
		if err := analysis.cancelled(); err != nil {
			return err
		}
		step := compositeScriptStep(metadataStep, parser)
		analysis.actions.observeCheckout(step)
		if _, action := step.Exec.(*ExecAction); action {
			for _, pass := range analysis.passes {
				if parent, enabled := pass.(*RuleAction); enabled {
					validation := NewRuleAction(analysis.actions)
					validation.SetConfig(parent.Config())
					if err := validation.VisitStep(step); err != nil {
						return err
					}
					analysis.rules = append(analysis.rules, compositeScriptRules{meta, []Rule{validation}})
					break
				}
			}
			if err := analysis.visitActionScripts(step, children, active); err != nil {
				return err
			}
			continue
		}
		for _, rule := range children {
			compositeCheckoutPaths(rule, analysis.actions)
			if err := rule.VisitStep(step); err != nil {
				return err
			}
		}
	}
	for i, parent := range parents {
		if outer, ok := parent.(*RuleExecutableBit); ok {
			if inner, ok := children[i].(*RuleExecutableBit); ok {
				outer.joinComposite(call, inner)
			}
		}
	}
	return nil
}

func compositeCheckoutPaths(rule Rule, actions *LocalActionsCache) {
	var paths *runPaths
	switch rule := rule.(type) {
	case *RuleShellcheck:
		paths = &rule.paths
	case *RuleExecutableBit:
		paths = &rule.paths
	default:
		return
	}
	checkout := actions.currentCheckout()
	paths.checkout, paths.checkoutUnknown = checkout.path, checkout.kind == directoryUnknown
	paths.placements = actions.checkoutState()
}

func compositeActionOrigin(paths *runPaths, call *Step, actionPath string) {
	paths.actionPath = actionPath
	paths.actionRunnerPath, paths.actionIndependent = "", false
	if action, ok := call.Exec.(*ExecAction); ok && action.Uses != nil {
		_, paths.actionIndependent = selfRepositoryUsesLocalSpec(action.Uses.Value)
		if !paths.actionIndependent {
			paths.actionRunnerPath = strings.TrimPrefix(action.Uses.Value, "./")
			placement := paths.placements.matching(paths.actionRunnerPath)
			if placement != nil && placement.caseInsensitive && placement.directory.kind == directoryKnown && !placement.foreign {
				relative, err := filepath.Rel(paths.workspace, actionPath)
				if err == nil && filepath.IsLocal(relative) {
					paths.actionRunnerPath = joinRunnerPath(placement.directory.path, filepath.ToSlash(relative))
				}
			}
		}
	}
}

func compositeScriptRule(parent Rule, call *Step, actionPath string, actions *LocalActionsCache) Rule {
	var child Rule
	switch rule := parent.(type) {
	case *RuleShellcheck:
		scoped := newRuleShellcheck(rule.cmd)
		scoped.config, scoped.paths, scoped.onInput = rule.config, rule.paths, rule.onInput
		scoped.platform = rule.platform
		// Resolve configuration anew: nested actions have different action_path
		// values even when they inherit the same configuration selection.
		scoped.actionPath = actionPath
		compositeCheckoutPaths(scoped, actions)
		compositeActionOrigin(&scoped.paths, call, actionPath)
		child = scoped
	case *RulePyflakes:
		child = newRulePyflakes(rule.cmd)
	case *RuleExecutableBit:
		child = rule.forkComposite(call, actionPath, actions)
	default:
		panic("unsupported composite script rule")
	}
	child.SetConfig(parent.Config())
	return child
}

// Composite steps require their own shell and do not inherit defaults.run.
// Relative working-directory values resolve from github.workspace.
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
	node, ok := compositeScriptNode(metadata.node, make(map[*yaml.Node]bool), make(map[*yaml.Node]*yaml.Node))
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
func compositeScriptNode(node *yaml.Node, active map[*yaml.Node]bool, done map[*yaml.Node]*yaml.Node) (*yaml.Node, bool) {
	node = actionSchemaNode(node)
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
		resolved, ok := compositeScriptNode(child, active, done)
		if !ok {
			return nil, false
		}
		cloned.Content[i] = resolved
	}
	done[node] = &cloned
	return &cloned, true
}
