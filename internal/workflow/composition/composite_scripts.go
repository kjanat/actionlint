package composition

import (
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/source"

	checks "actionlint.kjanat.dev/v2/internal/checks"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func (analysis *compositeAnalyzer) visitActionScripts(call workflowdecl.StepView, parents []builtin.InvocationParticipant, validate func() error) (err error) {
	if err := analysis.sourceSnapshot.ValidateStep(call); err != nil {
		return err
	}
	if err := analysis.cancelled(); err != nil {
		return err
	}
	if analysis.sourceSnapshot == nil {
		return errors.New("action call source snapshot is unavailable")
	}
	declaration := call
	actionView, ok := declaration.Action()
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
		return analysis.visitOpaqueAction(call, declaration, actionView, parents)
	}
	callContext, admitted := ticket.Context()
	if !admitted {
		return invocation.ErrForeignContext
	}
	callFrame := *analysis
	callFrame.context = callContext
	opened := false
	defer func() {
		err = analysis.interrupt(err)
		if opened {
			if closeErr := analysis.invocations.Close(callContext); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	meta, sourceDocument, outcome, err := analysis.resolveAction(actionView)
	if err != nil {
		return err
	}
	outcome, err = analysis.invocations.Resolve(callContext, outcome)
	if err != nil {
		return err
	}
	if outcome.Kind() != invocation.OutcomeExpanded {
		return callFrame.visitOpaqueAction(call, declaration, actionView, parents)
	}
	opened = true
	target, ok := outcome.Target()
	if !ok {
		return errors.New("expanded action has no source reference")
	}
	snapshot, ok := analysis.sourceSnapshots.Lookup(target)
	if !ok {
		return errors.New("expanded action source snapshot is unavailable")
	}
	return callFrame.visitMetadataScripts(meta, snapshot, sourceDocument.Address(), callContext, parents)
}

func (analysis *compositeAnalyzer) visitOpaqueAction(call workflowdecl.StepView, declaration workflowdecl.StepView, actionView workflowdecl.ExecActionView, parents []builtin.InvocationParticipant) error {
	analysis.actions.ObserveOpaqueAction(declaration, actionView)
	for _, rule := range parents {
		compositeCheckoutPaths(rule, analysis.actions)
		visit := func() error {
			return checks.VisitWithOrigin(rule, canonicalStepOrigin(declaration, analysis.jobID), checks.HasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitOpaqueAction(call) })
		}
		var err error
		if analysis.scriptScope != nil && analysis.scriptScope.Composite {
			err = invocationOutput(rule, analysis.sourceSnapshot, analysis.scriptScope.Deployment.SourcePath(), call.Reference(), visit)
		} else {
			err = visit()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) visitMetadataScripts(meta *actionmeta.Metadata, admitted *documentparse.SourceSnapshot, address string, callContext invocation.Context, parents []builtin.InvocationParticipant) (err error) {
	if !analysis.invocations.Owns(callContext) {
		return invocation.ErrForeignContext
	}
	if analysis.sourceSnapshots == nil {
		return errors.New("action source registry is unavailable")
	}
	if admitted == nil || admitted.Registry() != analysis.sourceSnapshots {
		return errors.New("action source snapshot belongs to a different registry")
	}
	snapshot, err := analysis.sourceSnapshots.Action(admitted.Capture(), meta)
	if err != nil {
		return err
	}
	body, available := snapshot.ActionView()
	if !available {
		return fmt.Errorf("action %q has no indexed declarations", meta.Path())
	}
	actions, err := analysis.actions.ForkComposite(callContext)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, analysis.actions.JoinComposite(actions)) }()

	scriptConfig := analysis.scriptConfig
	if analysis.configForFile != nil {
		var err error
		scriptConfig, err = analysis.configForFile(address)
		if err != nil {
			return err
		}
	}
	deployment, err := scriptexecution.NewDeployment(snapshot, address, meta.Path(), meta.SourceMapped)
	if err != nil {
		return err
	}
	parentScope := analysis.scriptScope
	scope := &scriptexecution.Scope{Paths: analysis.scriptPaths, Config: scriptConfig, Deployment: deployment, Composite: true, Context: callContext}
	if parentScope != nil {
		scope.Paths = parentScope.Paths
	}
	scope.Paths.Placements = actions.CheckoutState()
	execution.ActionOrigin(&scope.Paths, callContext, deployment.ActionPath())
	child := *analysis
	child.actions = actions
	child.sourceSnapshot = snapshot
	child.scriptConfig = scriptConfig
	child.scriptScope = scope
	child.context = callContext
	var entered []builtin.InvocationParticipant
	defer func() {
		for _, e := range slices.Backward(entered) {
			err = errors.Join(err, invocationOutput(e, snapshot, meta.Path(), snapshot.Reference(), func() error { return e.LeaveInvocation(callContext) }))
		}
	}()
	for _, participant := range parents {
		compositeCheckoutPaths(participant, actions)
		if err := invocationOutput(participant, snapshot, meta.Path(), snapshot.Reference(), func() error {
			return participant.EnterInvocation(builtin.Invocation{Context: callContext, ActionPath: deployment.ActionPath(), Source: snapshot, Config: scriptConfig, Path: meta.Path()})
		}); err != nil {
			return err
		}
		entered = append(entered, participant)
	}
	if callContext.IsEntry() && len(analysis.scriptScopes) != 0 {
		analysis.scriptScopes[len(analysis.scriptScopes)-1] = scope
	} else {
		analysis.scriptScopes = append(analysis.scriptScopes, scope)
	}
	for step := range body.Steps() {
		if err := analysis.cancelled(); err != nil {
			return err
		}
		declaration := step
		actions.ObserveExecution(declaration)
		actions.ObserveCheckout(declaration)
		if err := child.collectScript(step, scriptConfig, nil, nil); err != nil {
			return err
		}
		if _, action := declaration.Action(); action {
			if err := child.visitActionScripts(step, parents, func() error { return child.validateMetadataStep(step, scriptConfig) }); err != nil {
				return err
			}
			continue
		}
		for _, rule := range entered {
			compositeCheckoutPaths(rule, actions)
			if err := invocationOutput(rule, snapshot, meta.Path(), step.Reference(), func() error {
				return checks.VisitWithOrigin(rule, canonicalStepOrigin(declaration, analysis.jobID), checks.HasConfiguredIgnores(scriptConfig), func() error { return rule.VisitInvocationStep(step) })
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) validateMetadataStep(step workflowdecl.StepView, config *repoconfig.Config) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	if analysis.sourceSnapshot == nil {
		return errors.New("metadata step source snapshot is unavailable")
	}
	declaration := step
	for _, pass := range analysis.passes {
		if parent, enabled := pass.(builtin.MetadataParticipant); enabled {
			validation := parent.ForMetadata(analysis.actions)
			err := invocationOutput(validation, analysis.sourceSnapshot, analysis.scriptScope.Deployment.SourcePath(), step.Reference(), func() error {
				return checks.VisitWithOrigin(validation, canonicalStepOrigin(declaration, analysis.jobID), checks.HasConfiguredIgnores(config), func() error { return validation.VisitStep(step) })
			})
			analysis.metadataFindings = append(analysis.metadataFindings, validation.Errs()...)
			return err
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

func (analysis *compositeAnalyzer) describeCall(step workflowdecl.StepView) (source.Reference, invocation.Facts, error) {
	if analysis.sourceSnapshot == nil {
		return source.Reference{}, invocation.Facts{}, errors.New("call source snapshot is unavailable")
	}
	site := step.Reference()
	declaration := step
	facts, err := execution.CallFacts(declaration, analysis.actions.Platform())
	return site, facts, err
}

func invocationOutput(rule workflowvisit.Rule, snapshot *documentparse.SourceSnapshot, path string, reference source.Reference, visit func() error) error {
	before := len(rule.Errs())
	defer func() {
		for _, finding := range rule.Errs()[before:] {
			if origin := finding.SuppressionOrigin; origin != nil && origin.Path == "" {
				origin.Path = path
			}
			if finding.Path == "" {
				finding.Path = path
				finding.Source = snapshot.Content()
				if finding.Reference == nil {
					finding.Reference = new(reference)
				}
			}
		}
	}()
	return visit()
}
