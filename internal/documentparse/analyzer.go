package documentparse

import (
	"actionlint.kjanat.dev/v2/analyzer"
	documentmodel "actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/source"
)

// AnalyzerSource projects the parsed source header without execution facts.
func (d Document) AnalyzerSource(path string, content []byte, reference source.Reference) analyzer.Input {
	input := analyzer.Input{Reference: reference, Available: []analyzer.Fact{analyzer.Source}, Path: path, Content: string(content), Kind: "workflow", ParseStatus: "failed"}
	switch outline := d.Outline.(type) {
	case documentmodel.Workflow:
		input.ParseStatus = outline.ParseStatus
	case documentmodel.Action:
		input.Kind = "action"
		input.ParseStatus = outline.ParseStatus
	}
	return input
}
