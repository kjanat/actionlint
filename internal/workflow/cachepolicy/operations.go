package cachepolicy

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// MetadataLookup provides reusable workflow metadata without owning discovery or policy.
type MetadataLookup interface {
	FindMetadataForCall(string) (*calls.Metadata, error)
}

type cacheModeVisit struct {
	spec string
	mode workflowlang.CacheModeKind
}

type operationCheck struct {
	cache    MetadataLookup
	findings []diagnostic.Diagnostic
}

func (r *operationCheck) Errorf(pos workflowlang.Pos, format string, args ...any) {
	r.findings = append(r.findings, report(pos, "cache-operation", format, args...))
}

// CheckCallOperations applies the caller's ceiling to inherited callee operations.
// Results belong to the call site; policy evaluation is never cached with metadata.
func CheckCallOperations(call declaration.WorkflowCallView, granted declaration.CacheModeView, lookup MetadataLookup) []diagnostic.Diagnostic {
	if !call.Present() || !call.Uses().Present() || lookup == nil {
		return nil
	}
	r := operationCheck{cache: lookup}
	r.checkCallOperations(position(call.Uses()), call.Uses().Value(), grant(granted), map[cacheModeVisit]bool{})
	return r.findings
}

// checkCallOperations reports inherited restrictions at the call site, where
// inline suppression and policy configuration apply. Metadata stores callee
// operations; each traversal evaluates them against the caller's ceiling.
func (r *operationCheck) checkCallOperations(pos workflowlang.Pos, sourceSpec string, granted modeGrant, checked map[cacheModeVisit]bool) {
	spec, local := references.LocalWorkflow(sourceSpec)
	if !local {
		return
	}
	have, known := granted.capabilities()
	if granted.present && !known {
		return
	}
	visit := cacheModeVisit{spec: spec}
	if granted.present {
		visit.mode = granted.kind
	}
	if checked[visit] {
		return
	}
	checked[visit] = true
	m, err := r.cache.FindMetadataForCall(sourceSpec)
	if err != nil || m == nil {
		// The workflow-call rule reports lookup errors.
		return
	}
	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		job := m.JobCacheAccess[id]
		own := metadataGrant(job.Mode)
		mode := effectiveGrant(granted, own)
		want, valid := mode.capabilities()
		if mode.present && !valid || known && valid && want&^have != 0 {
			// Invalid declarations and ceiling mismatches have their own diagnostics.
			continue
		}
		if !own.present && known {
			for _, name := range job.Operations {
				if operation := disabledCacheOperation(name, have); operation != "" {
					r.Errorf(pos, "%q in job %q of %q cannot %s with caller-imposed cache-mode %q. GitHub skips the operation; select an action and mode that match the intended cache access, or document a reviewed exception at this call", name, id, sourceSpec, operation, granted.kind.String())
				}
			}
		}
		// An explicit intermediate mode owns its downstream operation diagnostics.
		if job.Uses != "" && !own.present {
			r.checkCallOperations(pos, job.SourceUses, mode, checked)
		}
	}
}

// CheckStep reports cache action operations disabled by an explicit cache mode.
func CheckStep(step declaration.StepView, mode declaration.CacheModeView) []diagnostic.Diagnostic {
	action, ok := step.Action()
	if !ok || !action.Uses().Present() {
		return nil
	}
	access, valid := grant(mode).capabilities()
	if !valid {
		return nil
	}
	name := references.CacheAction(action.Uses().Value())
	if operation := disabledCacheOperation(name, access); operation != "" {
		return []diagnostic.Diagnostic{report(position(action.Uses()), "cache-operation", "%q cannot %s with effective cache-mode %q. GitHub skips the operation; remove this cache step or select an action and mode that match the job's intended cache access", name, operation, mode.Kind().String())}
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
