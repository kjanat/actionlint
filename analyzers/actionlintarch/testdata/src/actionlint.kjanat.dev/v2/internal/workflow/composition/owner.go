package composition // want package:"project dependency paths"

import "actionlint.kjanat.dev/v2/analyzer"

type wrapper struct{ analyzer.Invocation }

func ownerWrites(inv *analyzer.Invocation, w *wrapper) {
	inv.Step.Line = 2
	inv.Step.Column++
	w.Step.Line--
	(&inv.Step).Line = 2
	for inv.Step.Line = range 3 {
	}
}
