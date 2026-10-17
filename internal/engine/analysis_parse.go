package engine

import (
	"actionlint.kjanat.dev/analyzer"
	actionmeta "actionlint.kjanat.dev/internal/action"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// parsedDocument preserves an outline and syntax findings even when traversal is impossible.
type parsedDocument struct {
	workflow *workflowlang.Workflow
	metadata *actionmeta.Metadata
	outline  DocumentOutline
	findings []*findingmodel.Finding
}

func parseDocument(path, configPath string, content []byte) parsedDocument {
	var w *workflowlang.Workflow
	var outline DocumentOutline
	var all []*findingmodel.Finding
	var metadata *actionmeta.Metadata
	if isActionInput(configPath) {
		var err error
		metadata, err = actionmeta.Parse(configPath, content)
		outline = actionOutline(path, metadata, err)
		if err != nil {
			all = adaptSyntaxFindings(workflowlang.HandleYAMLUnmarshalError(err))
			for _, finding := range all {
				finding.Start.Line, finding.Start.Column = max(1, finding.Start.Line), max(1, finding.Start.Column)
			}
		} else {
			w = &workflowlang.Workflow{}
		}
	} else {
		w, all = Parse(content)
		outline = workflowOutline(path, w, len(all) != 0)
	}
	return parsedDocument{w, metadata, outline, all}
}

func (p parsedDocument) analyzerInput(path, configPath string, content []byte, units []scriptUnit, facts factAvailability, sources map[string][]byte) analyzer.Input {
	workflow := p.workflow
	if p.metadata != nil && workflow != nil {
		workflow = actionFactsWorkflow(p.metadata)
	}
	return documentAnalyzerInput(path, configPath, content, workflow, p.outline, units, facts, sources)
}
