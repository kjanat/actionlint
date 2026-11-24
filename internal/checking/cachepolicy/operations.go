package cachepolicy

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/cachegrant"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

type ReusableWorkflowMetadataLookup interface {
	LookupForCall(string) (*calls.Metadata, error)
}

type cacheModeVisit struct {
	spec string
	mode definition.CacheModeKind
}

type operationCheck struct {
	cache    ReusableWorkflowMetadataLookup
	findings []results.Diagnostic
}

func (r *operationCheck) Errorf(pos sourcetext.Position, format string, args ...any) {
	r.findings = append(r.findings, report(pos, "cache-operation", format, args...))
}

// CheckCallOperations results belong to the call site; policy evaluation is never cached with metadata.
func CheckCallOperations(call *definition.WorkflowCall, granted *definition.CacheMode, lookup ReusableWorkflowMetadataLookup) []results.Diagnostic {
	if call == nil || call.Uses == nil || lookup == nil {
		return nil
	}
	r := operationCheck{cache: lookup}
	r.checkCallOperations(sourcetext.ZeroIfNil(call.Uses.Pos), call.Uses.Value, cachegrant.FromDeclaration(granted), map[cacheModeVisit]bool{})
	return r.findings
}

func (r *operationCheck) checkCallOperations(pos sourcetext.Position, sourceSpec string, granted cachegrant.Grant, checked map[cacheModeVisit]bool) {
	spec, local := callreferences.LocalWorkflow(sourceSpec)
	if !local {
		return
	}
	have, known := granted.Capabilities()
	if granted.Present() && !known {
		return
	}
	visit := cacheModeVisit{spec: spec}
	if granted.Present() {
		visit.mode = granted.Kind()
	}
	if checked[visit] {
		return
	}
	checked[visit] = true
	m, err := r.cache.LookupForCall(sourceSpec)
	if err != nil || m == nil {
		// The workflow-call rule reports lookup errors.
		return
	}
	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		job := m.JobCacheAccess[id]
		own := cachegrant.FromMetadata(job.Mode)
		mode := granted.Override(own)
		want, valid := mode.Capabilities()
		if mode.Present() && !valid || known && valid && want&^have != 0 {
			// Invalid declarations and ceiling mismatches have their own diagnostics.
			continue
		}
		if !own.Present() && known {
			for _, name := range job.Operations {
				if operation := disabledCacheOperation(name, have); operation != "" {
					r.Errorf(pos, "%q in job %q of %q cannot %s with caller-imposed cache-mode %q. GitHub skips the operation; select an action and mode that match the intended cache access, or document a reviewed exception at this call", name, id, sourceSpec, operation, granted.Kind().String())
				}
			}
		}
		if job.Uses != "" && !own.Present() {
			r.checkCallOperations(pos, job.SourceUses, mode, checked)
		}
	}
}

func CheckStep(step workflows.BoundStep, mode *definition.CacheMode) []results.Diagnostic {
	action, ok := step.Action()
	if !ok || action.Uses == nil {
		return nil
	}
	access, valid := cachegrant.FromDeclaration(mode).Capabilities()
	if !valid {
		return nil
	}
	name := callreferences.CacheAction(action.Uses.Value)
	if operation := disabledCacheOperation(name, access); operation != "" {
		return []results.Diagnostic{report(sourcetext.ZeroIfNil(action.Uses.Pos), "cache-operation", "%q cannot %s with effective cache-mode %q. GitHub skips the operation; remove this cache step or select an action and mode that match the job's intended cache access", name, operation, mode.Kind.String())}
	}
	return nil
}

func disabledCacheOperation(name string, access uint8) string {
	if name == "" {
		return ""
	}
	_, rest, _ := strings.Cut(name, "/")
	_, path, _ := strings.Cut(rest, "/")
	switch path {
	case "save":
		if access&2 == 0 {
			return "save caches"
		}
	case "restore":
		if access&1 == 0 {
			return "restore caches"
		}
	case "":
		if access == 0 {
			return "restore or save caches"
		}
	}
	return ""
}
