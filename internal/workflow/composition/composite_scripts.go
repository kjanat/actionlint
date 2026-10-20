package composition

import (
	"path/filepath"
	"strings"

	checks "actionlint.kjanat.dev/v2/internal/checks"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
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
			if err := checks.VisitWithOrigin(rule, findingmodel.StepOrigin(analysis.jobID, call), checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitStep(call) }); err != nil {
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
	scope := &scriptexecution.Scope{Paths: analysis.scriptPaths, Config: scriptConfig, Composite: true, Document: meta.Path(), Call: call, Depth: 1}
	if parentScope != nil {
		scope.Paths = parentScope.Paths
		scope.Caller, scope.Depth = parentScope.Document, parentScope.Depth+1
	}
	scope.ActionPath = filepath.Dir(meta.Path())
	if meta.SourceMapped {
		scope.ActionPath = ""
	}
	scope.Paths.Placements = analysis.actions.CheckoutState()
	execution.ActionOrigin(&scope.Paths, call, scope.ActionPath)
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
		participant, ok := parent.(builtin.CompositeParticipant)
		if !ok {
			continue
		}
		child, err := participant.ForComposite(builtin.Invocation{Call: call, ActionPath: actionPath, Actions: analysis.actions})
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
		step := documentparse.ActionStep(metadataStep, parser)
		analysis.actions.ObserveExecution(step)
		analysis.actions.ObserveCheckout(step)
		if err := analysis.collectScript(step, scriptConfig, meta.Path(), meta.Source, nil, nil); err != nil {
			return err
		}
		if _, action := step.Exec.(*workflowlang.ExecAction); action {
			for _, pass := range analysis.passes {
				if parent, enabled := pass.(builtin.MetadataParticipant); enabled {
					validation := parent.ForMetadata(analysis.actions)
					if err := checks.VisitWithOrigin(validation, findingmodel.StepOrigin(analysis.jobID, step), checks.HasConfiguredIgnores(scriptConfig), func() error { return validation.VisitStep(step) }); err != nil {
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
			if err := checks.VisitWithOrigin(rule, findingmodel.StepOrigin(analysis.jobID, step), checks.HasConfiguredIgnores(scriptConfig), func() error { return rule.VisitStep(step) }); err != nil {
				return err
			}
		}
	}
	for i, parent := range participants {
		if joiner, ok := parent.(builtin.CompositeJoiner); ok {
			joiner.JoinCompositeResult(call, children[i])
		}
	}
	return nil
}

func compositeCheckoutPaths(rule workflowvisit.Pass, actions *actionlocal.Resolver) {
	participant, ok := rule.(builtin.CheckoutParticipant)
	if !ok {
		return
	}
	compositeCheckoutScope(participant.CheckoutPaths(), actions)
}

func compositeCheckoutScope(paths *execution.Paths, actions *actionlocal.Resolver) {
	*paths = actions.ApplyCheckout(*paths)
}
