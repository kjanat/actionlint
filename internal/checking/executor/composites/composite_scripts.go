package composites

import (
	"errors"
	"fmt"
	"slices"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	"actionlint.kjanat.dev/v2/internal/input/source"

	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/selection"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func (analysis *compositeAnalyzer) visitActionScripts(call workflowdecl.StepView, parents []checkcalls.InvocationParticipant, validate func() error) (err error) {
	if err := analysis.sourceSnapshot.ValidateStep(call); err != nil {
		return err
	}
	if err := analysis.cancelled(); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
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
	callFrame.environmentLayers = append(slices.Clone(analysis.environmentLayers), scriptmodel.DeclaredEnvironment(scriptmodel.EnvironmentCaller, call.Env().Mapping()))
	opened := false
	defer func() {
		err = analysis.interrupt(err)
		if opened {
			if closeErr := analysis.invocations.Close(callContext); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	if err := analysis.actions.PrepareMetadata(analysis.ctx, actionView.Uses().Value()); err != nil {
		return err
	}
	prepared, _ := analysis.actions.Prepared().LookupMetadata(actionView.Uses().Value())
	if errors.Is(prepared.Err(), errRemoteMetadataLookupLimit) {
		return prepared.Err()
	}
	if document, found := analysis.actions.SourceDocument(prepared); found {
		revision := document.Capture().Revision()
		analysis.recordUncheckedFiles(prepared.Metadata(), document.ReferencedFiles(), source.Reference{Document: revision.Document(), Revision: revision.Digest()}, callContext)
	}
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	meta, address, outcome, err := analysis.resolveAction(actionView)
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
	// Check the call in its declaring invocation before entering the child.
	// Opaque calls receive the same visit through visitOpaqueAction.
	if analysis.scriptScope != nil && analysis.scriptScope.Composite {
		for _, parent := range parents {
			if participant, ok := parent.(checkcalls.ExpandedActionParticipant); ok {
				if err := invocationOutput(participant, analysis.sourceSnapshot, analysis.scriptScope.Deployment.SourcePath(), call.Reference(), func() error {
					return selection.VisitWithOrigin(participant, canonicalStepOrigin(call, analysis.jobID), selection.HasConfiguredIgnores(analysis.scriptConfig), func() error { return participant.VisitExpandedAction(call) })
				}); err != nil {
					return err
				}
			}
		}
	}
	target, ok := outcome.Target()
	if !ok {
		return errors.New("expanded action has no source reference")
	}
	snapshot, ok := analysis.sourceSnapshots.Lookup(target)
	if !ok {
		return errors.New("expanded action source snapshot is unavailable")
	}
	return callFrame.visitMetadataScripts(meta, snapshot, address, callContext, parents)
}

func (analysis *compositeAnalyzer) visitOpaqueAction(call workflowdecl.StepView, declaration workflowdecl.StepView, actionView workflowdecl.ExecActionView, parents []checkcalls.InvocationParticipant) error {
	analysis.actions.ObserveOpaqueAction(declaration, actionView)
	for _, rule := range parents {
		compositeCheckoutPaths(rule, analysis.actions)
		visit := func() error {
			return selection.VisitWithOrigin(rule, canonicalStepOrigin(declaration, analysis.jobID), selection.HasConfiguredIgnores(analysis.scriptConfig), func() error { return rule.VisitOpaqueAction(call) })
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

func (analysis *compositeAnalyzer) visitMetadataScripts(meta *actionmeta.Metadata, admitted parsing.SourceSnapshot, address string, callContext invocation.Context, parents []checkcalls.InvocationParticipant) (err error) {
	if !analysis.invocations.Owns(callContext) {
		return invocation.ErrForeignContext
	}
	if analysis.sourceSnapshots == nil {
		return errors.New("action source registry is unavailable")
	}
	if !analysis.sourceSnapshots.OwnsSnapshot(admitted) {
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
	if err := analysis.prepareDependencies(snapshot, callContext.Depth()); err != nil {
		return err
	}
	actions, err := analysis.actions.ForkComposite(callContext)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, analysis.actions.JoinComposite(actions)) }()
	if document, remote := analysis.remoteDocuments[address]; remote {
		actions.SetRepositorySource(analysis.repositorySource(document.identity))
	}

	scriptConfig := analysis.scriptConfig
	if analysis.configForFile != nil && !source.RemoteAddress(address) {
		var err error
		scriptConfig, err = analysis.configForFile(address)
		if err != nil {
			return err
		}
	}
	deployment, err := scriptexecution.NewDeployment(analysis.sourceSnapshots, snapshot, address, meta.Path(), meta.SourceMapped)
	if err != nil {
		return err
	}
	parentScope := analysis.scriptScope
	scope := &scriptexecution.Scope{Paths: analysis.scriptPaths, Reader: analysis.reader, Config: scriptConfig, ConfigProvenance: analysis.configProvenance, Deployment: deployment, Composite: true, Context: callContext}
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
	var entered []checkcalls.InvocationParticipant
	defer func() {
		for _, e := range slices.Backward(entered) {
			err = errors.Join(err, invocationOutput(e, snapshot, meta.Path(), snapshot.Reference(), func() error { return e.LeaveInvocation(callContext) }))
		}
	}()
	for _, participant := range parents {
		compositeCheckoutPaths(participant, actions)
		if err := invocationOutput(participant, snapshot, meta.Path(), snapshot.Reference(), func() error {
			return participant.EnterInvocation(checkcalls.Invocation{Context: callContext, ActionPath: deployment.ActionPath(), Source: snapshot, Config: scriptConfig, Path: meta.Path(), Actions: actions.Prepared()})
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
		if err := child.visitInvocationStep(step, entered); err != nil {
			return err
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) visitInvocationStep(step workflowdecl.StepView, participants []checkcalls.InvocationParticipant) error {
	for _, participant := range participants {
		compositeCheckoutPaths(participant, analysis.actions)
		if err := invocationOutput(participant, analysis.sourceSnapshot, analysis.scriptScope.Deployment.SourcePath(), step.Reference(), func() error {
			return selection.VisitWithOrigin(participant, canonicalStepOrigin(step, analysis.jobID), selection.HasConfiguredIgnores(analysis.scriptConfig), func() error { return participant.VisitInvocationStep(step) })
		}); err != nil {
			return err
		}
	}
	return nil
}

func (analysis *compositeAnalyzer) validateMetadataStep(step workflowdecl.StepView, config *repoconfig.Config) error {
	if err := analysis.sourceSnapshot.ValidateStep(step); err != nil {
		return err
	}
	if !analysis.sourceSnapshot.Present() {
		return errors.New("metadata step source snapshot is unavailable")
	}
	declaration := step
	for _, pass := range analysis.passes {
		if parent, enabled := pass.(checkcalls.MetadataParticipant); enabled {
			validation := parent.ForMetadata(analysis.actions)
			err := invocationOutput(validation, analysis.sourceSnapshot, analysis.scriptScope.Deployment.SourcePath(), step.Reference(), func() error {
				return selection.VisitWithOrigin(validation, canonicalStepOrigin(declaration, analysis.jobID), selection.HasConfiguredIgnores(config), func() error { return validation.VisitStep(step) })
			})
			analysis.metadataFindings = append(analysis.metadataFindings, validation.Errs()...)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func compositeCheckoutPaths(rule workflowvisit.Pass, actions *workspaceactions.Resolver) {
	participant, ok := rule.(checkcalls.CheckoutParticipant)
	if !ok {
		return
	}
	participant.ApplyCheckout(actions)
}

func (analysis *compositeAnalyzer) describeCall(step workflowdecl.StepView) (source.Reference, invocation.Facts, error) {
	if !analysis.sourceSnapshot.Present() {
		return source.Reference{}, invocation.Facts{}, errors.New("call source snapshot is unavailable")
	}
	site := step.Reference()
	declaration := step
	facts, err := execution.CallFacts(declaration, analysis.actions.Platform())
	return site, facts, err
}

func invocationOutput(rule workflowvisit.Rule, snapshot parsing.SourceSnapshot, path string, reference source.Reference, visit func() error) error {
	defer rule.AnnotateSince(rule.FindingCount(), func(finding *findingmodel.Finding) {
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
	})
	return visit()
}
