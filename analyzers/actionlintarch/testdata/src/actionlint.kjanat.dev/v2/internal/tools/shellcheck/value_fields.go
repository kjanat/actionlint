package shellcheck

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type container struct{ Value analyzer.Invocation }
type positionAlias = diagnostic.Position

func nestedValues(inv *analyzer.Invocation, w *wrapper, o *outer, c *container) {
	inv.Step.Line = 2             // want "ALA103"
	inv.Step.Column++             // want "ALA103"
	(inv.Step).Line--             // want "ALA103"
	inv.Step.Column += 2          // want "ALA103"
	(*inv).Step.Line = 2          // want "ALA103"
	w.Step.Line = 2               // want "ALA103"
	o.Step.Column++               // want "ALA103"
	c.Value.Step.Line = 2         // want "ALA103"
	for inv.Step.Line = range 3 { // want "ALA103"
	}
	for _, w.Step.Column = range []int{1, 2} { // want "ALA103"
	}
	(&inv.Step).Line = 2    // want "ALA103"
	(*(&inv.Step)).Line = 2 // want "ALA103"
	*(&inv.Step.Line) = 2   // want "ALA103"
	(*(&inv.Step.Line))++   // want "ALA103"
	_ = inv.Step.Line
	copy := inv.Step
	copy.Line = 2
	var alias positionAlias = inv.Step
	alias.Column++
}
