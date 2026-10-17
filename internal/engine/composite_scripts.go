package engine

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/internal/workflow/references"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

// Each invocation owns its rules: external callbacks must not share mutable
// diagnostic origins, and the same metadata can run in different caller contexts.
type compositeScriptRules struct {
	meta  *ActionMetadata
	rules []Rule
}

func (analysis *compositeAnalyzer) visitActionScripts(call *workflowlang.Step, parents []Rule, active map[string]bool) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	action, ok := call.Exec.(*workflowlang.ExecAction)
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
		analysis.actions.observeOpaqueAction(call, action)
		for _, rule := range parents {
			compositeCheckoutPaths(rule, analysis.actions)
			if err := visitWithIgnoreOrigin(rule, newDiagnosticIgnoreOrigin(analysis.jobID, call), hasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitStep(call) }); err != nil {
				return err
			}
		}
		return nil
	}
	return analysis.visitMetadataScripts(meta, call, parents, active)
}

func (analysis *compositeAnalyzer) visitMetadataScripts(meta *ActionMetadata, call *workflowlang.Step, parents []Rule, active map[string]bool) error {
	active[meta.Path()] = true
	defer delete(active, meta.Path())
	checkout := analysis.actions.checkoutState()
	checkoutEnv := analysis.actions.checkoutEnvUnknown
	persistentEnv := analysis.actions.persistentEnvUnknown
	shellEnv, checkoutShell := analysis.actions.shellEnvUnknown, analysis.actions.checkoutShell
	analysis.actions.checkoutEnvUnknown = checkoutEnv || checkoutEnvironmentUnknown(call.Env, analysis.actions.platform)
	analysis.actions.shellEnvUnknown = shellEnv || analysis.actions.shellEnvironmentUnknown(call.Env)
	analysis.actions.checkoutShell = executionShell{}
	defer func() {
		analysis.actions.checkoutEnvUnknown = checkoutEnv
		analysis.actions.shellEnvUnknown, analysis.actions.checkoutShell = shellEnv, checkoutShell
		enabled, known := invocationCondition(call.If)
		if known && !enabled {
			analysis.actions.persistentEnvUnknown = persistentEnv
			analysis.actions.restoreCheckout(checkout)
		} else if (!known || boolMayBeTrue(call.ContinueOnError) || boolMayBeTrue(call.Background)) && checkout != analysis.actions.checkoutState() {
			mayFail := boolMayBeTrue(call.ContinueOnError) || boolMayBeTrue(call.Background)
			analysis.actions.restoreCheckout(mergeCheckoutBranches(checkout, analysis.actions.checkoutState(), mayFail))
		}
	}()

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
	if meta.sourceMapped {
		scope.actionPath = ""
	}
	scope.paths.placements = analysis.actions.checkoutState()
	compositeActionOrigin(&scope.paths, call, scope.actionPath)
	analysis.scriptScope = scope
	analysis.scriptScopes = append(analysis.scriptScopes, scope)
	defer func() { analysis.scriptScope = parentScope }()
	children := make([]Rule, 0, len(parents))
	var participants []Rule
	for _, parent := range parents {
		actionPath := filepath.Dir(meta.Path())
		if meta.sourceMapped {
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
	parser := workflowlang.NewSourceParser(meta.src)
	parser.IndexScalarSources(meta.schemaRoot)
	for _, metadataStep := range meta.Runs.Steps {
		if err := analysis.cancelled(); err != nil {
			return err
		}
		step := compositeScriptStep(metadataStep, parser)
		analysis.actions.observeExecution(step)
		analysis.actions.observeCheckout(step)
		analysis.collectScript(step, scriptConfig, meta.Path(), meta.src, nil, nil)
		if _, action := step.Exec.(*workflowlang.ExecAction); action {
			for _, pass := range analysis.passes {
				if parent, enabled := pass.(metadataParticipant); enabled {
					validation := parent.forMetadata(analysis.actions)
					if err := visitWithIgnoreOrigin(validation, newDiagnosticIgnoreOrigin(analysis.jobID, step), hasConfiguredIgnores(scriptConfig), func() error { return validation.VisitStep(step) }); err != nil {
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
			if err := visitWithIgnoreOrigin(rule, newDiagnosticIgnoreOrigin(analysis.jobID, step), hasConfiguredIgnores(scriptConfig), func() error { return rule.VisitStep(step) }); err != nil {
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

func compositeCheckoutPaths(rule Pass, actions *LocalActionsCache) {
	participant, ok := rule.(checkoutParticipant)
	if !ok {
		return
	}
	paths := participant.checkoutPaths()
	checkout := actions.currentCheckout()
	paths.checkout, paths.checkoutUnknown = checkout.path, checkout.kind == directoryUnknown
	paths.placements = actions.checkoutState()
	paths.caseInsensitive = actions.caseInsensitive
}

func compositeActionOrigin(paths *runPaths, call *workflowlang.Step, actionPath string) {
	paths.actionPath = actionPath
	paths.actionRunnerPath, paths.actionIndependent = "", false
	if action, ok := call.Exec.(*workflowlang.ExecAction); ok && action.Uses != nil {
		_, paths.actionIndependent = references.SelfRepositoryLocal(action.Uses.Value)
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

// Composite steps require their own shell and do not inherit defaults.run.
// Relative working-directory values resolve from github.workspace.
// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs
func compositeScriptStep(metadata *ActionCompositeStep, parser *workflowlang.Parser) *workflowlang.Step {
	pos := &workflowlang.Pos{Line: metadata.Line, Col: metadata.Column}
	opaque := &workflowlang.Step{Pos: pos, Exec: &workflowlang.ExecAction{}}
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
	parser.Errors = nil
	step := parser.ParseStep(actionMetadataFields(node))
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
