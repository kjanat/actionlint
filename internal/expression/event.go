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

// missingEventProperty gives a bounded diagnostic instead of printing an entire
// webhook schema. Dot and literal bracket accesses use the same field lookup.
func (sema *Checker) missingEventProperty(node, receiver Node, name string, object *ObjectType) bool {
	path := eventReference(receiver)
	if path != "github.event" && !strings.HasPrefix(path, "github.event.") {
		return false
	}
	suggestion := ""
	for candidate := range object.Props {
		if adjacentTypo(name, candidate) {
			if suggestion != "" {
				suggestion = ""
				break
			}
			suggestion = candidate
		}
	}
	if suggestion != "" {
		sema.errorf(unknownEventProperty, node, "property %q is not defined in %s; did you mean %q?", name, path, suggestion)
	} else {
		sema.errorf(unknownEventProperty, node, "property %q is not defined in %s for the configured workflow events", name, path)
	}
	return true
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
	return &ObjectType{Props: maps.Clone(event.Props), Mapped: event.Mapped, DisplayName: event.DisplayName}
}

// eventReference retains numeric and wildcard paths for payload diagnostics.
func eventReference(n Node) string {
	switch n := n.(type) {
	case *ArrayDerefNode:
		return eventReference(n.Receiver) + ".*"
	case *IndexAccessNode:
		if _, ok := n.Index.(*StringNode); !ok {
			return eventReference(n.Operand) + "[]"
		}
	case *ObjectDerefNode:
		if base := eventReference(n.Receiver); base != "" {
			return base + "." + n.Property
		}
	}
	return conditionReference(n)
}
