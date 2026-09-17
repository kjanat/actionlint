package actionlint

import (
	"maps"
	"slices"
)

type workflowCacheModeVisit struct {
	spec string
	mode CacheModeKind
}

func (rule *RuleWorkflowCall) checkWorkflowCallCacheMode(pos *Pos, spec, sourceSpec string, granted *CacheMode, m *ReusableWorkflowMetadata, checked map[workflowCacheModeVisit]bool) {
	have, known := granted.capabilities()
	if granted != nil && !known {
		return
	}
	visit := workflowCacheModeVisit{spec: spec}
	if granted != nil {
		visit.mode = granted.Kind
	}
	if checked[visit] {
		return
	}
	checked[visit] = true

	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		access := m.JobCacheAccess[id]
		mode := effectiveCacheMode(granted, access.Mode)
		want, valid := mode.capabilities()
		if mode != nil && !valid {
			continue
		}
		if known && valid && want&^have != 0 {
			rule.Errorf(pos, "nested job %q of %q requests cache-mode %q but the calling job allows %q", id, sourceSpec, mode.Kind.String(), granted.Kind.String())
			continue
		}
		if access.Uses == "" {
			continue
		}
		next, err := rule.cache.findMetadataForCall(access.SourceUses)
		if err != nil {
			rule.Errorf(pos, "could not validate cache access through job %q of %q: %s", id, sourceSpec, err)
			continue
		}
		if next == nil {
			rule.Debug("Skip nested cache-mode check for %q: metadata unavailable", access.Uses)
			continue
		}
		rule.checkWorkflowCallCacheMode(pos, access.Uses, access.SourceUses, mode, next, checked)
	}
}
