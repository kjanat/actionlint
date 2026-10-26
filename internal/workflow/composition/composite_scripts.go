package composition

import (
	"errors"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/source"

	checks "actionlint.kjanat.dev/v2/internal/checks"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// Each invocation owns its rules: external callbacks must not share mutable
// diagnostic origins, and the same metadata can run in different caller contexts.
type compositeScriptRules struct {
	meta  *actionmeta.Metadata
	rules []workflowvisit.Rule
}

func (analysis *compositeAnalyzer) visitActionScripts(call *workflowlang.Step, parents []workflowvisit.Rule, validate func() error) (err error) {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	action, ok := call.Exec.(*workflowlang.ExecAction)
	if !ok {
		return nil
	}
	site, facts, err := analysis.describeCall(call)
	if err != nil {
		return err
	}
	ticket, err := analysis.invocations.Begin(analysis.context, site, facts)
	if err != nil {
		return err
	}
	if ticket.Status() == invocation.Stopped {
		return ticket.Err()
	}
	if ticket.Status() == invocation.DepthLimited {
		var deferred []workflowvisit.Rule
		for _, rule := range parents {
			if participant, ok := rule.(builtin.InvocationParticipant); ok && participant.DeferActionInvocation() {
				deferred = append(deferred, rule)
			}
		}
		return analysis.visitOpaqueAction(call, action, deferred)
	}
	callContext, admitted := ticket.Context()
	if !admitted {
		return invocation.ErrForeignContext
	}
	parent := analysis.context
	analysis.context = callContext
	opened := false
	defer func() {
		err = analysis.interrupt(err)
		if opened {
			if closeErr := analysis.invocations.Close(callContext); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		analysis.context = parent
	}()
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	meta, outcome, err := analysis.resolveAction(action)
	if err != nil {
		return err
	}
	outcome, err = analysis.invocations.Resolve(callContext, outcome)
	if err != nil {
		return err
	}
	if outcome.Kind() != invocation.OutcomeExpanded {
		return analysis.visitOpaqueAction(call, action, parents)
	}
	opened = true
	return analysis.visitMetadataScripts(meta, callContext, parents)
}

func (analysis *compositeAnalyzer) visitOpaqueAction(call *workflowlang.Step, action *workflowlang.ExecAction, parents []workflowvisit.Rule) error {
	analysis.actions.ObserveOpaqueAction(call, action)
	for _, rule := range parents {
		compositeCheckoutPaths(rule, analysis.actions)
		if err := checks.VisitWithOrigin(rule, findingmodel.StepOrigin(analysis.jobID, call), checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitStep(call) }); err != nil {
			return err
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) visitMetadataScripts(meta *actionmeta.Metadata, callContext invocation.Context, parents []workflowvisit.Rule) error {
	if !analysis.invocations.Owns(callContext) {
		return invocation.ErrForeignContext
	}
	if analysis.sourceSnapshots == nil {
		analysis.sourceSnapshots = &documentparse.SourceSnapshots{}
	}
	snapshot, err := analysis.sourceSnapshots.Action(fsread.PhysicalPath(meta.Path()), meta)
	if err != nil {
		return err
	}
	steps, available := snapshot.ActionSteps()
	if !available {
		return fmt.Errorf("action %q has no indexed declarations", meta.Path())
	}
	parentSnapshot := analysis.sourceSnapshot
	analysis.sourceSnapshot = snapshot
	defer func() { analysis.sourceSnapshot = parentSnapshot }()
	defer analysis.actions.EnterComposite(callContext)()

	scriptConfig := analysis.scriptConfig
	if analysis.configForFile != nil {
		var err error
		scriptConfig, err = analysis.configForFile(meta.Path())
		if err != nil {
			return err
		}
	}
	parentConfig := analysis.scriptConfig
	analysis.scriptConfig = scriptConfig
	defer func() { analysis.scriptConfig = parentConfig }()
	parentScope := analysis.scriptScope
	scope := &scriptexecution.Scope{Paths: analysis.scriptPaths, Config: scriptConfig, Composite: true, Context: callContext}
	if parentScope != nil {
		scope.Paths = parentScope.Paths
	}
	scope.ActionPath = filepath.Dir(meta.Path())
	if meta.SourceMapped {
		scope.ActionPath = ""
	}
	scope.Paths.Placements = analysis.actions.CheckoutState()
	execution.ActionOrigin(&scope.Paths, callContext, scope.ActionPath)
	analysis.scriptScope = scope
	if callContext.IsEntry() && len(analysis.scriptScopes) != 0 {
		analysis.scriptScopes[len(analysis.scriptScopes)-1] = scope
	} else {
		analysis.scriptScopes = append(analysis.scriptScopes, scope)
	}
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
		child, err := participant.ForComposite(builtin.Invocation{Context: callContext, ActionPath: actionPath, Actions: analysis.actions})
		if err != nil {
			return err
		}
		children = append(children, child)
		participants = append(participants, parent)
	}
	analysis.rules = append(analysis.rules, compositeScriptRules{meta, children})
	for _, step := range steps {
		if err := analysis.cancelled(); err != nil {
			return err
		}
		analysis.actions.ObserveExecution(step)
		analysis.actions.ObserveCheckout(step)
		if err := analysis.collectScript(step, scriptConfig, meta.Path(), meta.Source, nil, nil); err != nil {
			return err
		}
		if _, action := step.Exec.(*workflowlang.ExecAction); action {
			if err := analysis.visitActionScripts(step, children, func() error { return analysis.validateMetadataStep(meta, step, scriptConfig) }); err != nil {
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
			joiner.JoinCompositeResult(callContext, children[i])
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) validateMetadataStep(meta *actionmeta.Metadata, step *workflowlang.Step, config *repoconfig.Config) error {
	for _, pass := range analysis.passes {
		if parent, enabled := pass.(builtin.MetadataParticipant); enabled {
			validation := parent.ForMetadata(analysis.actions)
			if err := checks.VisitWithOrigin(validation, findingmodel.StepOrigin(analysis.jobID, step), checks.HasConfiguredIgnores(config), func() error { return validation.VisitStep(step) }); err != nil {
				return err
			}
			analysis.rules = append(analysis.rules, compositeScriptRules{meta, []workflowvisit.Rule{validation}})
			break
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

func (analysis *compositeAnalyzer) describeCall(step *workflowlang.Step) (source.Reference, invocation.Facts, error) {
	if analysis.sourceSnapshot == nil {
		return source.Reference{}, invocation.Facts{}, errors.New("call source snapshot is unavailable")
	}
	site, err := analysis.sourceSnapshot.StepReference(step)
	if err != nil {
		return source.Reference{}, invocation.Facts{}, err
	}
	facts, err := execution.CallFacts(step, analysis.actions.Platform())
	return site, facts, err
}
