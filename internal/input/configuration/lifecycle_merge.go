package config

import configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"

// mergeLifecycleLint overlays lifecycle policies by ID while replacing each policy whole.
func mergeLifecycleLint(base, overlay any) any {
	merged := configtree.Merge(base, overlay)
	b, bok := base.(map[string]any)
	o, ook := overlay.(map[string]any)
	m, mok := merged.(map[string]any)
	if !bok || !ook || !mok {
		return merged
	}
	bl, _ := b["lifecycle"].(map[string]any)
	ol, ok := o["lifecycle"].(map[string]any)
	if !ok {
		return merged
	}
	old, _ := bl["policies"].([]any)
	add, ok := ol["policies"].([]any)
	if !ok {
		return merged
	}
	if len(add) == 0 {
		return merged
	}
	combined := append([]any(nil), old...)
	indexes := map[string]int{}
	for i, v := range combined {
		if p, ok := v.(map[string]any); ok {
			if id, ok := p["id"].(string); ok {
				indexes[id] = i
			}
		}
	}
	for _, v := range add {
		p, ok := v.(map[string]any)
		if !ok {
			combined = append(combined, v)
			continue
		}
		id, _ := p["id"].(string)
		if i, ok := indexes[id]; ok {
			combined[i] = v
		} else {
			indexes[id] = len(combined)
			combined = append(combined, v)
		}
	}
	lm := cloneMapping(m["lifecycle"])
	setField(lm, "policies", combined)
	setField(m, "lifecycle", lm)
	return m
}
