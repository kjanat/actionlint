package invocation

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/source"
)

// Script binds one declaration to the entry or expanded call executing it.
type Script struct {
	ID          ID
	Owner       ID
	Declaration source.Reference
	Kind        ScriptKind
	Input       string
}

// AddScript registers a declaration while its owner is the current traversal body.
func (b *Builder) AddScript(context Context, declaration source.Reference, kind ScriptKind, input string) (ID, error) {
	if !b.Owns(context) {
		return "", ErrForeignContext
	}
	owner := context.ID()
	if b.published {
		return "", ErrPublished
	}
	if b.interrupted {
		return "", ErrInterrupted
	}
	body := b.entry.Reference
	if owner != b.root {
		index, exists := b.index[owner]
		if !exists {
			return "", fmt.Errorf("%w: script owner %q", ErrUnknownCall, owner)
		}
		call := b.records[index]
		if call.state != stateOpen {
			return "", fmt.Errorf("%w: script owner %q", ErrParentNotOpen, owner)
		}
		body, _ = call.call.Outcome.Target()
	}
	if owner != b.current {
		return "", fmt.Errorf("%w: script owner %q is not current", ErrTraversal, owner)
	}
	if declaration.Document != body.Document || declaration.Revision != body.Revision {
		return "", fmt.Errorf("%w: script declaration does not belong to its owner's source snapshot", ErrInvalidSource)
	}
	id, err := NewScriptID(owner, declaration, kind, input)
	if err != nil {
		return "", err
	}
	if _, duplicate := b.scriptIndex[id]; duplicate {
		return "", fmt.Errorf("%w: %q", ErrDuplicateScript, id)
	}
	if b.hasStop {
		return "", budgetError{b.stop}
	}
	if len(b.scripts) >= b.budget.MaxScripts {
		b.stop = Stop{Parent: owner, Site: declaration, Limit: LimitScripts, Max: b.budget.MaxScripts}
		b.hasStop = true
		return "", budgetError{b.stop}
	}
	if b.scriptIndex == nil {
		b.scriptIndex = make(map[ID]int)
	}
	b.scriptIndex[id] = len(b.scripts)
	b.scripts = append(b.scripts, Script{ID: id, Owner: owner, Declaration: declaration, Kind: kind, Input: input})
	return id, nil
}

// Scripts returns registered occurrences in traversal order.
func (g Graph) Scripts() []Script { return slices.Clone(g.scripts) }

// Script returns a registered occurrence; computed but unregistered IDs are absent.
func (g Graph) Script(id ID) (Script, bool) {
	index, exists := g.scriptIndex[id]
	if !exists {
		return Script{}, false
	}
	return g.scripts[index], true
}
