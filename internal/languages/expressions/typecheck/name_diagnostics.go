package typecheck

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/results/messages"

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
	var candidates []string
	for candidate := range object.Properties() {
		candidates = append(candidates, candidate)
	}
	suggestion := messages.CloseName(name, candidates)
	if suggestion != "" {
		sema.errorf(kind, node, "property %q is not defined in %s; did you mean %q?", name, path, object.PropertyName(suggestion))
	} else {
		sema.errorf(kind, node, "property %q is not defined in %s%s", name, path, suffix)
	}
}
