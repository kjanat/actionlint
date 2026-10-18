package expression

import (
	"maps"
	"strings"
)

// UpdateEvent installs a caller-owned payload projection without copying its
// potentially large schema graph. It is treated as immutable by the checker.
func (sema *Checker) UpdateEvent(event *ObjectType) {
	sema.ensureGithubVarCopied()
	if gh, ok := sema.vars["github"].(*ObjectType); ok {
		gh.Props["event"] = event
	}
}

// missingProperty gives a bounded diagnostic for dot, literal bracket and filtered access.
func (sema *Checker) missingProperty(node, receiver Node, name string, object *ObjectType) {
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
	for candidate := range object.Props {
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

// copyEventObject isolates dispatch-input updates from shared payload shapes.
func copyEventObject(event *ObjectType) *ObjectType {
	return &ObjectType{Props: maps.Clone(event.Props), Mapped: event.Mapped, DisplayName: event.DisplayName, PropertyNames: maps.Clone(event.PropertyNames), EventVariants: event.EventVariants}
}

// eventReference retains numeric and wildcard paths for payload diagnostics.
func eventReference(n Node) string {
	switch n := n.(type) {
	case *VariableNode:
		if n.tok != nil {
			return n.tok.Value
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
