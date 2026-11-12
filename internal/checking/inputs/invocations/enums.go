package invocations

import (
	"fmt"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

func projectEntryKind(value invocation.EntryKind) (checkinputs.InvocationEntryKind, error) {
	switch value {
	case invocation.EntryWorkflow:
		return checkinputs.WorkflowEntry, nil
	case invocation.EntryAction:
		return checkinputs.ActionEntry, nil
	default:
		return "", fmt.Errorf("%w: unsupported EntryKind %q", checkinputs.ErrInvalidCallGraph, value)
	}
}

func projectScriptKind(value invocation.ScriptKind) (checkinputs.ScriptKind, error) {
	switch value {
	case invocation.ScriptRun:
		return checkinputs.RunScript, nil
	case invocation.ScriptActionInput:
		return checkinputs.ActionInputScript, nil
	default:
		return "", fmt.Errorf("%w: unsupported ScriptKind %q", checkinputs.ErrInvalidCallGraph, value)
	}
}

// opaqueReasons projects the reasons a callee is opaque.
var opaqueReasons = map[invocation.Reason]checkinputs.OpaqueReason{
	invocation.ReasonDockerfileOnly: checkinputs.DockerfileOnly,
	invocation.ReasonNonComposite:   checkinputs.NonComposite,
}

func projectOpaqueReason(value invocation.Reason) (checkinputs.OpaqueReason, error) {
	if reason, ok := opaqueReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported OpaqueReason %q", checkinputs.ErrInvalidCallGraph, value)
}

// unavailableReasons projects the reasons a callee is unavailable.
var unavailableReasons = map[invocation.Reason]checkinputs.UnavailableReason{
	invocation.ReasonNonLocal:           checkinputs.NonLocal,
	invocation.ReasonNotFetched:         checkinputs.NotFetched,
	invocation.ReasonLookupDisabled:     checkinputs.LookupDisabled,
	invocation.ReasonPlacementUncertain: checkinputs.PlacementUncertain,
}

func projectUnavailableReason(value invocation.Reason) (checkinputs.UnavailableReason, error) {
	if reason, ok := unavailableReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported UnavailableReason %q", checkinputs.ErrInvalidCallGraph, value)
}

// unresolvedReasons projects the reasons a call is unresolved.
var unresolvedReasons = map[invocation.Reason]checkinputs.UnresolvedReason{
	invocation.ReasonInvalidCall:     checkinputs.InvalidCall,
	invocation.ReasonMissingManifest: checkinputs.MissingManifest,
	invocation.ReasonReadFailed:      checkinputs.ReadFailed,
	invocation.ReasonParseFailed:     checkinputs.ParseFailed,
	invocation.ReasonMappingFailed:   checkinputs.MappingFailed,
}

func projectUnresolvedReason(value invocation.Reason) (checkinputs.UnresolvedReason, error) {
	if reason, ok := unresolvedReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported UnresolvedReason %q", checkinputs.ErrInvalidCallGraph, value)
}

// interruptionReasons projects the reasons expansion was interrupted.
var interruptionReasons = map[invocation.Reason]checkinputs.InterruptionReason{
	invocation.ReasonCanceled:    checkinputs.Canceled,
	invocation.ReasonOperational: checkinputs.OperationalFailure,
}

func projectInterruptionReason(value invocation.Reason) (checkinputs.InterruptionReason, error) {
	if reason, ok := interruptionReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported InterruptionReason %q", checkinputs.ErrInvalidCallGraph, value)
}
