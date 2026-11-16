package backends

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// runBody returns a run step whose body is text and whose run key is at key.
func runBody(text string, key *sourcetext.Position) *definition.ExecRun {
	return &definition.ExecRun{Run: &definition.String{Value: text}, RunPos: key}
}

// actionInput returns the action input site whose value is text and whose
// input name is at key.
func actionInput(site checkinputs.ScriptInput, text string, key *sourcetext.Position) *scriptanalysis.ActionInput {
	return &scriptanalysis.ActionInput{Site: site, Value: &definition.ActionArgument{Name: &definition.String{Value: site.Input, Pos: key}, Value: &definition.String{Value: text}}}
}
