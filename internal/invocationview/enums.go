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

func projectOpaqueReason(value invocation.Reason) (analyzer.OpaqueReason, error) {
	switch value {
	case invocation.ReasonDockerfileOnly:
		return analyzer.DockerfileOnly, nil
	case invocation.ReasonNonComposite:
		return analyzer.NonComposite, nil
	default:
		return "", fmt.Errorf("%w: unsupported OpaqueReason %q", analyzer.ErrInvalidCallGraph, value)
	}
}

func projectUnavailableReason(value invocation.Reason) (analyzer.UnavailableReason, error) {
	switch value {
	case invocation.ReasonNonLocal:
		return analyzer.NonLocal, nil
	case invocation.ReasonNotFetched:
		return analyzer.NotFetched, nil
	case invocation.ReasonLookupDisabled:
		return analyzer.LookupDisabled, nil
	case invocation.ReasonPlacementUncertain:
		return analyzer.PlacementUncertain, nil
	default:
		return "", fmt.Errorf("%w: unsupported UnavailableReason %q", analyzer.ErrInvalidCallGraph, value)
	}
}

func projectUnresolvedReason(value invocation.Reason) (analyzer.UnresolvedReason, error) {
	switch value {
	case invocation.ReasonInvalidCall:
		return analyzer.InvalidCall, nil
	case invocation.ReasonMissingManifest:
		return analyzer.MissingManifest, nil
	case invocation.ReasonReadFailed:
		return analyzer.ReadFailed, nil
	case invocation.ReasonParseFailed:
		return analyzer.ParseFailed, nil
	case invocation.ReasonMappingFailed:
		return analyzer.MappingFailed, nil
	default:
		return "", fmt.Errorf("%w: unsupported UnresolvedReason %q", analyzer.ErrInvalidCallGraph, value)
	}
}

func projectInterruptionReason(value invocation.Reason) (analyzer.InterruptionReason, error) {
	switch value {
	case invocation.ReasonCanceled:
		return analyzer.Canceled, nil
	case invocation.ReasonOperational:
		return analyzer.OperationalFailure, nil
	default:
		return "", fmt.Errorf("%w: unsupported InterruptionReason %q", analyzer.ErrInvalidCallGraph, value)
	}
}
