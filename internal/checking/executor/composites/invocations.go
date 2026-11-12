package composites

import (
	"context"
	"errors"
	"fmt"
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func (analysis *compositeAnalyzer) startEntry(kind invocation.EntryKind, snapshot parsing.SourceSnapshot) error {
	if !snapshot.Present() {
		return errors.New("entry source snapshot is unavailable")
	}
	if analysis.sourceSnapshots == nil {
		return errors.New("entry source registry is unavailable")
	}
	if !analysis.sourceSnapshots.OwnsSnapshot(snapshot) {
		return errors.New("entry source snapshot belongs to a different registry")
	}
	graph, err := invocation.NewBuilder(kind, snapshot.Reference(), analysis.budget)
	if err != nil {
		return err
	}
	analysis.invocations, analysis.context, analysis.sourceSnapshot = graph, graph.RootContext(), snapshot
	if analysis.prepareSource != nil {
		return analysis.prepareSource(snapshot)
	}
	return nil
}

func (analysis *compositeAnalyzer) interrupt(err error) error {
	if err == nil || analysis.interrupted || analysis.invocations == nil {
		return err
	}
	if canceled := analysis.cancelled(); canceled != nil && !errors.Is(err, canceled) {
		err = errors.Join(err, canceled)
	}
	if analysis.invocations.IsBudgetStop(err) {
		return err
	}
	reason := invocation.ReasonOperational
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		reason = invocation.ReasonCanceled
	}
	_, interruptionErr := analysis.invocations.Interrupt(reason, err.Error())
	analysis.interrupted = interruptionErr == nil
	return errors.Join(err, interruptionErr)
}

func metadataOutcome(result workspaceactions.MetadataResult) (invocation.Outcome, error) {
	detail := ""
	if result.Err() != nil {
		detail = result.Err().Error()
	}
	switch result.Status() {
	case workspaceactions.MetadataDockerfileOnly:
		return invocation.Opaque(invocation.ReasonDockerfileOnly, detail)
	case workspaceactions.MetadataLookupDisabled:
		return invocation.Unavailable(invocation.ReasonLookupDisabled, detail)
	case workspaceactions.MetadataNonLocal:
		return invocation.Unavailable(invocation.ReasonNonLocal, detail)
	case workspaceactions.MetadataNotFetched:
		return invocation.Unavailable(invocation.ReasonNotFetched, detail)
	case workspaceactions.MetadataPlacementUncertain:
		return invocation.Unavailable(invocation.ReasonPlacementUncertain, detail)
	case workspaceactions.MetadataMissingManifest:
		return invocation.Unresolved(invocation.ReasonMissingManifest, detail)
	case workspaceactions.MetadataReadFailed:
		return invocation.Unresolved(invocation.ReasonReadFailed, detail)
	case workspaceactions.MetadataParseFailed:
		return invocation.Unresolved(invocation.ReasonParseFailed, detail)
	case workspaceactions.MetadataMappingFailed:
		return invocation.Unresolved(invocation.ReasonMappingFailed, detail)
	case workspaceactions.MetadataInvalid, workspaceactions.MetadataResolved:
	}
	return invocation.Outcome{}, fmt.Errorf("metadata lookup returned invalid outcome %d", result.Status())
}

func (analysis *compositeAnalyzer) resolveAction(action workflowdecl.ExecActionView) (*actionmeta.Metadata, string, invocation.Outcome, error) {
	uses := action.Uses()
	if !uses.Present() || uses.Value() == "" {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference is empty")
		return nil, "", outcome, err
	}
	spec := uses.Value()
	if exprsyntax.ContainsExpression(spec) {
		outcome, err := invocation.Dynamic(spec)
		return nil, "", outcome, err
	}
	position, hasPosition := uses.Pos()
	if invalidActionReference(spec, position, hasPosition) {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference has invalid syntax")
		return nil, "", outcome, err
	}
	lookup, _ := analysis.actions.Prepared().LookupMetadata(spec)
	if lookup.Status() == workspaceactions.MetadataNonLocal && analysis.remoteActions != nil {
		return analysis.resolveRemoteAction(spec)
	}
	if errors.Is(lookup.Err(), context.Canceled) || errors.Is(lookup.Err(), context.DeadlineExceeded) {
		return nil, "", invocation.Outcome{}, lookup.Err()
	}
	if err := analysis.cancelled(); err != nil {
		return nil, "", invocation.Outcome{}, err
	}
	sourceDocument, hasSource := analysis.actions.SourceDocument(lookup)
	var snapshot parsing.SourceSnapshot
	if hasSource {
		admitted, err := analysis.sourceSnapshots.Document(sourceDocument.Capture(), parsing.Document{Provenance: sourceDocument.Provenance()})
		if err != nil {
			return nil, sourceDocument.Address(), invocation.Outcome{}, err
		}
		snapshot = admitted
	}
	if lookup.Status() != workspaceactions.MetadataResolved {
		outcome, err := metadataOutcome(lookup)
		return nil, sourceDocument.Address(), outcome, err
	}
	meta := lookup.Metadata()
	if meta == nil {
		return nil, sourceDocument.Address(), invocation.Outcome{}, errors.New("resolved action metadata is unavailable")
	}
	if !snapshot.Present() {
		return nil, sourceDocument.Address(), invocation.Outcome{}, errors.New("resolved action source document is unavailable")
	}
	actionView, available := snapshot.ActionView()
	if !available {
		return nil, sourceDocument.Address(), invocation.Outcome{}, errors.New("resolved action source has no action view")
	}
	using := actionView.Using()
	if !strings.EqualFold(using, "composite") {
		outcome, err := invocation.Opaque(invocation.ReasonNonComposite, using)
		return meta, sourceDocument.Address(), outcome, err
	}
	outcome, err := invocation.Expanded(snapshot.Reference())
	return meta, sourceDocument.Address(), outcome, err
}

func invalidActionReference(spec string, position workflowlang.Pos, hasPosition bool) bool {
	return actioncheck.InvalidActionReference(spec, position, hasPosition)
}
