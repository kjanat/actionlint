package cachepolicy

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

type ceilingCheck struct {
	cache    MetadataLookup
	debug    func(string, ...any)
	findings []diagnostic.Diagnostic
}

func (r *ceilingCheck) Errorf(pos workflowlang.Pos, format string, args ...any) {
	r.findings = append(r.findings, report(pos, "workflow-call", format, args...))
}

func (r *ceilingCheck) Debug(format string, args ...any) {
	if r.debug != nil {
		r.debug(format, args...)
	}
}

// CheckCeiling verifies that callee jobs cannot exceed the caller's cache grant.
// It follows nested local calls and reports failures at the original call site.
func CheckCeiling(call declaration.WorkflowCallView, granted declaration.CacheModeView, metadata *calls.Metadata, lookup MetadataLookup, debug func(string, ...any)) []diagnostic.Diagnostic {
	if !call.Present() || !call.Uses().Present() || metadata == nil {
		return nil
	}
	spec, local := references.LocalWorkflow(call.Uses().Value())
	if !local {
		return nil
	}
	r := ceilingCheck{cache: lookup, debug: debug}
	r.check(position(call.Uses()), spec, call.Uses().Value(), grant(granted), metadata, map[cacheModeVisit]bool{})
	return r.findings
}

func (r *ceilingCheck) check(pos workflowlang.Pos, spec, sourceSpec string, granted modeGrant, m *calls.Metadata, checked map[cacheModeVisit]bool) {
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

	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		access := m.JobCacheAccess[id]
		mode := effectiveGrant(granted, metadataGrant(access.Mode))
		want, valid := mode.capabilities()
		if mode.present && !valid {
			continue
		}
		if known && valid && want&^have != 0 {
			r.Errorf(pos, "nested job %q of %q requests cache-mode %q but the calling job allows %q", id, sourceSpec, mode.kind.String(), granted.kind.String())
			continue
		}
		if access.Uses == "" {
			continue
		}
		next, err := r.cache.FindMetadataForCall(access.SourceUses)
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
