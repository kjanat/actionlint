// Package tree merges configuration values and records their sources through extends.
package tree

import "maps"

// Input identifies the source of a value and any inherited null reset.
type Input struct {
	Name, File         string
	Line, Column       int
	KeyLine, KeyColumn int
	Reset              *Input
}

// Merge overlays mappings recursively. Lists and scalars replace their base.
func Merge(base, overlay any) any {
	b, bok := base.(map[string]any)
	o, ook := overlay.(map[string]any)
	if !bok || !ook {
		return clone(overlay)
	}
	result := maps.Clone(b)
	for key, value := range o {
		if prior, ok := b[key]; ok {
			result[key] = Merge(prior, value)
		} else {
			result[key] = clone(value)
		}
	}
	return result
}

func clone(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = clone(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = clone(item)
		}
		return out
	default:
		return value
	}
}
