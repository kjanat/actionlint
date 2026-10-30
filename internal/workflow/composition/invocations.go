package composition

import (
	"context"
	"errors"
	"fmt"
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func (analysis *compositeAnalyzer) startEntry(kind invocation.EntryKind, snapshot *documentparse.SourceSnapshot) error {
	if snapshot == nil {
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

func metadataOutcome(result actionlocal.MetadataResult) (invocation.Outcome, error) {
	detail := ""
	if result.Err() != nil {
		detail = result.Err().Error()
	}
	switch result.Status() {
	case actionlocal.MetadataDockerfileOnly:
		return invocation.Opaque(invocation.ReasonDockerfileOnly, detail)
	case actionlocal.MetadataLookupDisabled:
		return invocation.Unavailable(invocation.ReasonLookupDisabled, detail)
	case actionlocal.MetadataNonLocal:
		return invocation.Unavailable(invocation.ReasonNonLocal, detail)
	case actionlocal.MetadataNotFetched:
		return invocation.Unavailable(invocation.ReasonNotFetched, detail)
	case actionlocal.MetadataPlacementUncertain:
		return invocation.Unavailable(invocation.ReasonPlacementUncertain, detail)
	case actionlocal.MetadataMissingManifest:
		return invocation.Unresolved(invocation.ReasonMissingManifest, detail)
	case actionlocal.MetadataReadFailed:
		return invocation.Unresolved(invocation.ReasonReadFailed, detail)
	case actionlocal.MetadataParseFailed:
		return invocation.Unresolved(invocation.ReasonParseFailed, detail)
	case actionlocal.MetadataMappingFailed:
		return invocation.Unresolved(invocation.ReasonMappingFailed, detail)
	case actionlocal.MetadataInvalid, actionlocal.MetadataResolved:
	}
	return invocation.Outcome{}, fmt.Errorf("metadata lookup returned invalid outcome %d", result.Status())
}

func (analysis *compositeAnalyzer) resolveAction(action workflowdecl.ExecActionView) (*actionmeta.Metadata, actionlocal.SourceDocument, invocation.Outcome, error) {
	uses := action.Uses()
	if !uses.Present() || uses.Value() == "" {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference is empty")
		return nil, actionlocal.SourceDocument{}, outcome, err
	}
	spec := uses.Value()
	if exprlang.ContainsExpression(spec) {
		outcome, err := invocation.Dynamic(spec)
		return nil, actionlocal.SourceDocument{}, outcome, err
	}
	position, hasPosition := uses.Pos()
	if invalidActionReference(spec, position, hasPosition) {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference has invalid syntax")
		return nil, actionlocal.SourceDocument{}, outcome, err
	}
	lookup, _ := analysis.actions.LookupMetadata(spec)
	if errors.Is(lookup.Err(), context.Canceled) || errors.Is(lookup.Err(), context.DeadlineExceeded) {
		return nil, actionlocal.SourceDocument{}, invocation.Outcome{}, lookup.Err()
	}
	if err := analysis.cancelled(); err != nil {
		return nil, actionlocal.SourceDocument{}, invocation.Outcome{}, err
	}
	sourceDocument, hasSource := analysis.actions.SourceDocument(lookup)
	var snapshot *documentparse.SourceSnapshot
	if hasSource {
		admitted, err := analysis.sourceSnapshots.Document(sourceDocument.Capture(), documentparse.Document{Provenance: sourceDocument.Provenance()})
		if err != nil {
			return nil, sourceDocument, invocation.Outcome{}, err
		}
		snapshot = admitted
	}
	if lookup.Status() != actionlocal.MetadataResolved {
		outcome, err := metadataOutcome(lookup)
		return nil, sourceDocument, outcome, err
	}
	meta := lookup.Metadata()
	if meta == nil {
		return nil, sourceDocument, invocation.Outcome{}, errors.New("resolved action metadata is unavailable")
	}
	if snapshot == nil {
		return nil, sourceDocument, invocation.Outcome{}, errors.New("resolved action source document is unavailable")
	}
	actionView, available := snapshot.ActionView()
	if !available {
		return nil, sourceDocument, invocation.Outcome{}, errors.New("resolved action source has no action view")
	}
	using := actionView.Using()
	if !strings.EqualFold(using, "composite") {
		outcome, err := invocation.Opaque(invocation.ReasonNonComposite, using)
		return meta, sourceDocument, outcome, err
	}
	outcome, err := invocation.Expanded(snapshot.Reference())
	return meta, sourceDocument, outcome, err
}

func invalidActionReference(spec string, position workflowlang.Pos, hasPosition bool) bool {
	return actioncheck.InvalidActionReference(spec, position, hasPosition)
}
