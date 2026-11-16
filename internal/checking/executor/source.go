package executor

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// SourceInput constructs a check input header without execution facts.
func SourceInput(document parsing.Document, path string, content []byte, reference source.Reference) checkinputs.AnalyzerInput {
	return checkinputs.AnalyzerInput{Reference: reference, AvailableInputs: []rule.InputKind{rule.Source}, Path: path, Content: string(content), Kind: document.Kind().String(), ParseStatus: document.ParseStatus()}
}
