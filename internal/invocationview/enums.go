package invocationview

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"fmt"
)

func projectEntryKind(value invocation.EntryKind) (analyzer.InvocationEntryKind, error) {
	switch value {
	case invocation.EntryWorkflow:
		return analyzer.WorkflowEntry, nil
	case invocation.EntryAction:
		return analyzer.ActionEntry, nil
	default:
		return "", fmt.Errorf("%w: unsupported EntryKind %q", analyzer.ErrInvalidCallGraph, value)
	}
}

func projectScriptKind(value invocation.ScriptKind) (analyzer.ScriptKind, error) {
	switch value {
	case invocation.ScriptRun:
		return analyzer.RunScript, nil
	case invocation.ScriptActionInput:
		return analyzer.ActionInputScript, nil
	default:
		return "", fmt.Errorf("%w: unsupported ScriptKind %q", analyzer.ErrInvalidCallGraph, value)
	}
}

// opaqueReasons projects the reasons a callee is opaque.
var opaqueReasons = map[invocation.Reason]analyzer.OpaqueReason{
	invocation.ReasonDockerfileOnly: analyzer.DockerfileOnly,
	invocation.ReasonNonComposite:   analyzer.NonComposite,
}

func projectOpaqueReason(value invocation.Reason) (analyzer.OpaqueReason, error) {
	if reason, ok := opaqueReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported OpaqueReason %q", analyzer.ErrInvalidCallGraph, value)
}

// unavailableReasons projects the reasons a callee is unavailable.
var unavailableReasons = map[invocation.Reason]analyzer.UnavailableReason{
	invocation.ReasonNonLocal:           analyzer.NonLocal,
	invocation.ReasonNotFetched:         analyzer.NotFetched,
	invocation.ReasonLookupDisabled:     analyzer.LookupDisabled,
	invocation.ReasonPlacementUncertain: analyzer.PlacementUncertain,
}

func projectUnavailableReason(value invocation.Reason) (analyzer.UnavailableReason, error) {
	if reason, ok := unavailableReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported UnavailableReason %q", analyzer.ErrInvalidCallGraph, value)
}

// unresolvedReasons projects the reasons a call is unresolved.
var unresolvedReasons = map[invocation.Reason]analyzer.UnresolvedReason{
	invocation.ReasonInvalidCall:     analyzer.InvalidCall,
	invocation.ReasonMissingManifest: analyzer.MissingManifest,
	invocation.ReasonReadFailed:      analyzer.ReadFailed,
	invocation.ReasonParseFailed:     analyzer.ParseFailed,
	invocation.ReasonMappingFailed:   analyzer.MappingFailed,
}

func projectUnresolvedReason(value invocation.Reason) (analyzer.UnresolvedReason, error) {
	if reason, ok := unresolvedReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported UnresolvedReason %q", analyzer.ErrInvalidCallGraph, value)
}

// interruptionReasons projects the reasons expansion was interrupted.
var interruptionReasons = map[invocation.Reason]analyzer.InterruptionReason{
	invocation.ReasonCanceled:    analyzer.Canceled,
	invocation.ReasonOperational: analyzer.OperationalFailure,
}

func projectInterruptionReason(value invocation.Reason) (analyzer.InterruptionReason, error) {
	if reason, ok := interruptionReasons[value]; ok {
		return reason, nil
	}
	return "", fmt.Errorf("%w: unsupported InterruptionReason %q", analyzer.ErrInvalidCallGraph, value)
}
