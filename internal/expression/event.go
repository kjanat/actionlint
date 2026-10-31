package expression

import "strings"

// UpdateEvent installs a payload projection as github.event. Types are
// immutable, so the potentially large schema graph is shared, not copied.
func (sema *Checker) UpdateEvent(event ObjectType) {
	sema.ensureVarsCopied()
	if gh, ok := sema.vars["github"].Object(); ok {
		sema.vars["github"] = gh.With("event", event.Type()).Type()
	}
}

// missingProperty gives a bounded diagnostic for dot, literal bracket and filtered access.
func (sema *Checker) missingProperty(node, receiver Node, name string, object ObjectType) {
	path := eventReference(receiver)
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

// eventReference formats a payload access path for diagnostics, using [] for
// non-string indexes and .* for array projections.
func eventReference(n Node) string {
	switch n := n.(type) {
	case *VariableNode:
		if n.First.Value != "" {
			return n.First.Value
		}
		return n.Name
	case *ArrayDerefNode:
		if base := eventReference(n.Receiver); base != "" {
			return base + ".*"
		}
	case *IndexAccessNode:
		if base := eventReference(n.Operand); base != "" {
			if lit, ok := n.Index.(*StringNode); ok {
				return base + "." + lit.Value
			}
			return base + "[]"
		}
	case *ObjectDerefNode:
		if base := eventReference(n.Receiver); base != "" {
			return base + "." + n.PropertyName()
		}
	}
	return ""
}
