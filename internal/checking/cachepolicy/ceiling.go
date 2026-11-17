package cachepolicy

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/cache"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

type ceilingCheck struct {
	cache    ReusableWorkflowMetadataLookup
	debug    func(string, ...any)
	findings []results.Diagnostic
}

func (r *ceilingCheck) Errorf(pos sourcetext.Position, format string, args ...any) {
	r.findings = append(r.findings, report(pos, "workflow-call", format, args...))
}

func (r *ceilingCheck) Debug(format string, args ...any) {
	if r.debug != nil {
		r.debug(format, args...)
	}
}

// CheckCeiling verifies that callee jobs cannot exceed the caller's cache grant.
// It follows nested local calls and reports failures at the original call site.
func CheckCeiling(call *definition.WorkflowCall, granted *definition.CacheMode, metadata *calls.Metadata, lookup ReusableWorkflowMetadataLookup, debug func(string, ...any)) []results.Diagnostic {
	if call == nil || call.Uses == nil || metadata == nil {
		return nil
	}
	spec, local := callreferences.LocalWorkflow(call.Uses.Value)
	if !local {
		return nil
	}
	r := ceilingCheck{cache: lookup, debug: debug}
	r.check(sourcetext.ZeroIfNil(call.Uses.Pos), spec, call.Uses.Value, cache.FromDeclaration(granted), metadata, map[cacheModeVisit]bool{})
	return r.findings
}

func (r *ceilingCheck) check(pos sourcetext.Position, spec, sourceSpec string, granted cache.Grant, m *calls.Metadata, checked map[cacheModeVisit]bool) {
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

	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		access := m.JobCacheAccess[id]
		mode := granted.Override(cache.FromMetadata(access.Mode))
		want, valid := mode.Capabilities()
		if mode.Present() && !valid {
			continue
		}
		if known && valid && want&^have != 0 {
			r.Errorf(pos, "nested job %q of %q requests cache-mode %q but the calling job allows %q", id, sourceSpec, mode.Kind().String(), granted.Kind().String())
			continue
		}
		if access.Uses == "" {
			continue
		}
		next, err := r.cache.LookupForCall(access.SourceUses)
		if err != nil {
			r.Errorf(pos, "could not validate cache access through job %q of %q: %s", id, sourceSpec, err)
			continue
		}
		if next == nil {
			r.Debug("Skip nested cache-mode check for %q: metadata unavailable", access.Uses)
			continue
		}
		r.check(pos, access.Uses, access.SourceUses, mode, next, checked)
	}
}
