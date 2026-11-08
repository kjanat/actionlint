package config

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

// absent fields return nil; explicit null uses this marker while inspecting a
// mapping. Stored documents and the binder always use ordinary nil values.
type explicitNull struct{}

func field(value any, key string) any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	child, found := object[key]
	if found && child == nil {
		return explicitNull{}
	}
	return child
}

func rawValue(value any) any {
	if _, ok := value.(explicitNull); ok {
		return nil
	}
	return value
}

func isNull(value any) bool    { return rawValue(value) == nil }
func isBool(value any) bool    { _, ok := value.(bool); return ok }
func isList(value any) bool    { _, ok := value.([]any); return ok }
func isMapping(value any) bool { _, ok := value.(map[string]any); return ok }
func mapping() map[string]any  { return map[string]any{} }
func cloneMapping(value any) any {
	if value == nil {
		return mapping()
	}
	if object, ok := value.(map[string]any); ok {
		return maps.Clone(object)
	}
	return value
}

func members(value any) map[string]any { object, _ := value.(map[string]any); return object }
func elements(value any) []any         { values, _ := value.([]any); return values }
func textValue(value string) any       { return value }
func text(value any) string            { result, _ := value.(string); return result }
func setField(value any, key string, child any) {
	object, ok := value.(map[string]any)
	if !ok {
		panic("configuration migration expected an object")
	}
	object[key] = rawValue(child)
}
func removeField(value any, key string)    { delete(members(value), key) }
func decodeValue(value any, dst any) error { return binding.Decode(rawValue(value), dst) }

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneObject(value)
	case []any:
		next := slices.Clone(value)
		for i, child := range next {
			next[i] = cloneValue(child)
		}
		return next
	default:
		return value
	}
}

func cloneObject(value map[string]any) map[string]any {
	next := make(map[string]any, len(value))
	for key, child := range value {
		next[key] = cloneValue(child)
	}
	return next
}
