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
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

func (analysis *compositeAnalyzer) startEntry(kind invocation.EntryKind, snapshot *documentparse.SourceSnapshot) error {
	graph, err := invocation.NewBuilder(kind, snapshot.Reference(), analysis.budget)
	if err != nil {
		return err
	}
	analysis.invocations, analysis.occurrence, analysis.sourceSnapshot = graph, graph.RootID(), snapshot
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
	default:
		return invocation.Outcome{}, fmt.Errorf("metadata lookup returned invalid outcome %d", result.Status())
	}
}

func (analysis *compositeAnalyzer) resolveAction(action *workflowlang.ExecAction) (*actionmeta.Metadata, invocation.Outcome, error) {
	if action.Uses == nil || action.Uses.Value == "" {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference is empty")
		return nil, outcome, err
	}
	if action.Uses.ContainsExpression() {
		outcome, err := invocation.Dynamic(action.Uses.Value)
		return nil, outcome, err
	}
	if invalidActionReference(action) {
		outcome, err := invocation.Unresolved(invocation.ReasonInvalidCall, "action reference has invalid syntax")
		return nil, outcome, err
	}
	lookup, _ := analysis.actions.LookupMetadata(action.Uses.Value)
	if errors.Is(lookup.Err(), context.Canceled) || errors.Is(lookup.Err(), context.DeadlineExceeded) {
		return nil, invocation.Outcome{}, lookup.Err()
	}
	if err := analysis.cancelled(); err != nil {
		return nil, invocation.Outcome{}, err
	}
	if lookup.Status() != actionlocal.MetadataResolved {
		outcome, err := metadataOutcome(lookup)
		return nil, outcome, err
	}
	meta := lookup.Metadata()
	if !strings.EqualFold(meta.Runs.Using, "composite") {
		outcome, err := invocation.Opaque(invocation.ReasonNonComposite, meta.Runs.Using)
		return meta, outcome, err
	}
	snapshot, err := analysis.sourceSnapshots.Action(fsread.PhysicalPath(meta.Path()), meta)
	if err != nil {
		return nil, invocation.Outcome{}, err
	}
	outcome, err := invocation.Expanded(snapshot.Reference())
	return meta, outcome, err
}

func invalidActionReference(action *workflowlang.ExecAction) bool {
	spec := action.Uses.Value
	switch {
	case strings.HasPrefix(spec, "./"):
		return false
	case strings.HasPrefix(spec, "$/"):
		_, valid := references.SelfRepositoryLocal(spec)
		return !valid
	case strings.HasPrefix(spec, "docker://"):
		return len(actioncheck.Docker(action)) != 0
	default:
		return len(actioncheck.Repository(action, nil, nil)) != 0
	}
}
