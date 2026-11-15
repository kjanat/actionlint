package tree

import (
	"fmt"
	"maps"
	"strings"
)

// mergeLifecycleOrigins keeps locations aligned with ID-based policy replacement.
func mergeLifecycleOrigins(dst, src map[string]Input, base, overlay any) {
	const path = "/lint/lifecycle/policies"
	get := func(v any) []any {
		root, _ := v.(map[string]any)
		lint, _ := root["lint"].(map[string]any)
		lifecycle, _ := lint["lifecycle"].(map[string]any)
		list, _ := lifecycle["policies"].([]any)
		return list
	}
	old, add := get(base), get(overlay)
	before := maps.Clone(dst)
	MergeOrigins(dst, src, base, overlay)
	if len(add) == 0 {
		return
	}
	ids := map[string]int{}
	for i, v := range old {
		if p, ok := v.(map[string]any); ok {
			if id, ok := p["id"].(string); ok {
				ids[id] = i
			}
		}
	}
	type entry struct {
		source map[string]Input
		index  int
	}
	entries := make([]entry, 0, len(old)+len(add))
	for i := range old {
		entries = append(entries, entry{before, i})
	}
	for i, v := range add {
		p, _ := v.(map[string]any)
		id, _ := p["id"].(string)
		if j, ok := ids[id]; ok {
			entries[j] = entry{src, i}
		} else {
			ids[id] = len(entries)
			entries = append(entries, entry{src, i})
		}
	}
	for k := range dst {
		if strings.HasPrefix(k, path+"/") {
			delete(dst, k)
		}
	}
	for target, e := range entries {
		prefix := fmt.Sprintf("%s/%d", path, e.index)
		dest := fmt.Sprintf("%s/%d", path, target)
		for k, v := range e.source {
			if k == prefix || strings.HasPrefix(k, prefix+"/") {
				dst[dest+strings.TrimPrefix(k, prefix)] = v
			}
		}
	}
}
