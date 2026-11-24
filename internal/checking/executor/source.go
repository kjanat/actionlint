package executor

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func SourceInput(document parsing.Document, path string, content []byte, reference sourcetext.Reference) checkinputs.AnalyzerInput {
	return checkinputs.AnalyzerInput{Reference: reference, AvailableInputs: []rule.InputKind{rule.Source}, Path: path, Content: string(content), Kind: document.Kind().String(), ParseStatus: document.ParseStatus()}
}
