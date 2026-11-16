package executor

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	rulemeta "actionlint.kjanat.dev/v2/internal/model/rules"
)

// SourceInput constructs a check input header without execution facts.
func SourceInput(document parsing.Document, path string, content []byte, reference source.Reference) checkinputs.AnalyzerInput {
	return checkinputs.AnalyzerInput{Reference: reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Source}, Path: path, Content: string(content), Kind: document.Kind().String(), ParseStatus: document.ParseStatus()}
}
