package scripts

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// runBody returns a run step whose body is text and whose run key is at key.
func runBody(text string, key *sourcepos.Position) *definition.ExecRun {
	return &definition.ExecRun{Run: &definition.String{Value: text}, RunPos: key}
}

// actionInput returns the action input site whose value is text and whose
// input name is at key.
func actionInput(site checkinputs.ScriptInput, text string, key *sourcepos.Position) *scriptexecution.ActionInput {
	return &scriptexecution.ActionInput{Site: site, Value: &definition.ActionArgument{Name: &definition.String{Value: site.Input, Pos: key}, Value: &definition.String{Value: text}}}
}
