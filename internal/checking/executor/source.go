package executor

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// SourceInput constructs a check input header without execution facts.
func SourceInput(document parsing.Document, path string, content []byte, reference source.Reference) checkinputs.Input {
	return checkinputs.Input{Reference: reference, AvailableInputs: []checkinputs.InputKind{checkinputs.Source}, Path: path, Content: string(content), Kind: string(document.Kind()), ParseStatus: document.ParseStatus()}
}
