package engine

import (
	"path/filepath"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/yaml"
)

// Each invocation owns its rules: external callbacks must not share mutable
// diagnostic origins, and the same metadata can run in different caller contexts.
type compositeScriptRules struct {
	meta  *actionmeta.Metadata
	rules []workflowvisit.Rule
}

func (analysis *compositeAnalyzer) visitActionScripts(call *workflowlang.Step, parents []workflowvisit.Rule, active map[string]bool) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	action, ok := call.Exec.(*workflowlang.ExecAction)
	if !ok {
		return nil
	}
	var meta *actionmeta.Metadata
	if action.Uses != nil && !action.Uses.ContainsExpression() {
		spec := action.Uses.Value
		// Metadata-load diagnostics belong to RuleAction. An unresolved action
		// still invalidates executable-bit assumptions below.
		meta, _, _ = analysis.actions.FindMetadata(spec)
	}
	if meta == nil || !strings.EqualFold(meta.Runs.Using, "composite") || active[meta.Path()] || len(active) >= 10 {
		analysis.actions.ObserveOpaqueAction(call, action)
		for _, rule := range parents {
			compositeCheckoutPaths(rule, analysis.actions)
			if err := visitWithIgnoreOrigin(rule, findingmodel.StepOrigin(analysis.jobID, call), hasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitStep(call) }); err != nil {
				return err
			}
		}
		return nil
	}
	return analysis.visitMetadataScripts(meta, call, parents, active)
}

func (analysis *compositeAnalyzer) visitMetadataScripts(meta *actionmeta.Metadata, call *workflowlang.Step, parents []workflowvisit.Rule, active map[string]bool) error {
	active[meta.Path()] = true
	defer delete(active, meta.Path())
	defer analysis.actions.EnterComposite(call)()

	scriptConfig := analysis.scriptConfig
	if analysis.collectScripts && analysis.configForFile != nil {
		var err error
		scriptConfig, err = analysis.configForFile(meta.Path())
		if err != nil {
			return err
		}
	}
	parentScope := analysis.scriptScope
	scope := &scriptScope{paths: analysis.scriptPaths, config: scriptConfig, composite: true, document: meta.Path(), call: call, depth: 1}
	if parentScope != nil {
		scope.paths = parentScope.paths
		scope.caller, scope.depth = parentScope.document, parentScope.depth+1
	}
	scope.actionPath = filepath.Dir(meta.Path())
	if meta.SourceMapped {
		scope.actionPath = ""
	}
	scope.paths.Placements = analysis.actions.CheckoutState()
	execution.ActionOrigin(&scope.paths, call, scope.actionPath)
	analysis.scriptScope = scope
	analysis.scriptScopes = append(analysis.scriptScopes, scope)
	defer func() { analysis.scriptScope = parentScope }()
	children := make([]workflowvisit.Rule, 0, len(parents))
	var participants []workflowvisit.Rule
	for _, parent := range parents {
		actionPath := filepath.Dir(meta.Path())
		if meta.SourceMapped {
			// Source metadata does not establish the generated action's runtime directory.
			actionPath = ""
		}
		participant, ok := parent.(compositeParticipant)
		if !ok {
			continue
		}
		child, err := participant.forComposite(compositeInvocation{call, actionPath, analysis.actions})
		if err != nil {
			return err
		}
		children = append(children, child)
		participants = append(participants, parent)
	}
	analysis.rules = append(analysis.rules, compositeScriptRules{meta, children})
	parser := workflowlang.NewSourceParser(meta.Source)
	parser.IndexScalarSources(meta.Root)
	for _, metadataStep := range meta.Runs.Steps {
		if err := analysis.cancelled(); err != nil {
			return err
		}
		step := compositeScriptStep(metadataStep, parser)
		analysis.actions.ObserveExecution(step)
		analysis.actions.ObserveCheckout(step)
		analysis.collectScript(step, scriptConfig, meta.Path(), meta.Source, nil, nil)
		if _, action := step.Exec.(*workflowlang.ExecAction); action {
			for _, pass := range analysis.passes {
				if parent, enabled := pass.(metadataParticipant); enabled {
					validation := parent.forMetadata(analysis.actions)
					if err := visitWithIgnoreOrigin(validation, findingmodel.StepOrigin(analysis.jobID, step), hasConfiguredIgnores(scriptConfig), func() error { return validation.VisitStep(step) }); err != nil {
						return err
					}
					analysis.rules = append(analysis.rules, compositeScriptRules{meta, []workflowvisit.Rule{validation}})
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
			if err := visitWithIgnoreOrigin(rule, findingmodel.StepOrigin(analysis.jobID, step), hasConfiguredIgnores(scriptConfig), func() error { return rule.VisitStep(step) }); err != nil {
				return err
			}
		}
	}
	for i, parent := range participants {
		if joiner, ok := parent.(compositeJoiner); ok {
			joiner.joinCompositeResult(call, children[i])
		}
	}
	return nil
}

func compositeCheckoutPaths(rule workflowvisit.Pass, actions *actionlocal.Resolver) {
	participant, ok := rule.(checkoutParticipant)
	if !ok {
		return
	}
	compositeCheckoutScope(participant.checkoutPaths(), actions)
}

func compositeCheckoutScope(paths *execution.Paths, actions *actionlocal.Resolver) {
	*paths = actions.ApplyCheckout(*paths)
}

// Composite steps require their own shell and do not inherit defaults.run.
// Relative working-directory values resolve from github.workspace.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func compositeScriptStep(metadata *actionmeta.Step, parser *workflowlang.Parser) *workflowlang.Step {
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
	node, ok := compositeScriptNode(metadata.Node, make(map[*yaml.Node]bool), make(map[*yaml.Node]*yaml.Node))
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
func compositeScriptNode(node *yaml.Node, active map[*yaml.Node]bool, done map[*yaml.Node]*yaml.Node) (*yaml.Node, bool) {
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
		resolved, ok := compositeScriptNode(child, active, done)
		if !ok {
			return nil, false
		}
		cloned.Content[i] = resolved
	}
	done[node] = &cloned
	return &cloned, true
}
