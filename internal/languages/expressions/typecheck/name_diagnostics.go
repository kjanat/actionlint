package typecheck

import (
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// missingProperty gives a bounded diagnostic for dot, literal bracket and filtered access.
func (sema *Checker) missingProperty(node, receiver exprsyntax.Node, name string, object exprtypes.ObjectType) {
	path := exprsyntax.DisplayReference(receiver)
	kind := unknownProperty
	suffix := ""
	key := strings.ToLower(path)
	if key == "github.event" || strings.HasPrefix(key, "github.event.") || strings.HasPrefix(key, "github.event[") {
		kind = unknownEventProperty
		suffix = " for the configured workflow events"
	}
	if path == "" {
		path = "this object"
	}
	suggestion := ""
	for candidate := range object.Properties() {
		if adjacentTypo(strings.ToLower(name), candidate) {
			if suggestion != "" {
				suggestion = ""
				break
			}
			suggestion = candidate
		}
	}
	if suggestion != "" {
		sema.errorf(kind, node, "property %q is not defined in %s; did you mean %q?", name, path, object.PropertyName(suggestion))
	} else {
		sema.errorf(kind, node, "property %q is not defined in %s%s", name, path, suffix)
	}
}

func adjacentTypo(a, b string) bool {
	if a == b {
		return false
	}
	x, y := []rune(a), []rune(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(y)-len(x) > 1 {
		return false
	}
	for i := range x {
		if x[i] == y[i] {
			continue
		}
		if len(x) < len(y) {
			return string(x[i:]) == string(y[i+1:])
		}
		if string(x[i+1:]) == string(y[i+1:]) {
			return true
		}
		return i+1 < len(x) && x[i] == y[i+1] && x[i+1] == y[i] && string(x[i+2:]) == string(y[i+2:])
	}
	return len(x) != len(y)
}

// CloseName suggests only a unique, single-edit candidate, independent of order.
func CloseName(name string, candidates []string) string {
	suggestion := ""
	for _, candidate := range candidates {
		if !adjacentTypo(strings.ToLower(name), strings.ToLower(candidate)) {
			continue
		}
		if suggestion != "" && !strings.EqualFold(suggestion, candidate) {
			return ""
		}
		suggestion = candidate
	}
	return suggestion
}
