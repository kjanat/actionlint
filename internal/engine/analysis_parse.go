package engine

import (
	"path/filepath"

	"actionlint.kjanat.dev/analyzer"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"go.yaml.in/yaml/v4"
)

// parsedDocument preserves an outline and syntax findings even when traversal is impossible.
type parsedDocument struct {
	workflow *workflowlang.Workflow
	metadata *ActionMetadata
	outline  DocumentOutline
	findings []*Error
}

func parseDocument(path, configPath string, content []byte) parsedDocument {
	var w *workflowlang.Workflow
	var outline DocumentOutline
	var all []*Error
	var metadata *ActionMetadata
	if isActionInput(configPath) {
		metadata = &ActionMetadata{}
		err := yaml.Unmarshal(content, metadata)
		metadata.dir, metadata.file, metadata.src = filepath.Dir(configPath), filepath.Base(configPath), content
		outline = actionOutline(path, metadata, err)
		if err != nil {
			all = adaptSyntaxFindings(workflowlang.HandleYAMLUnmarshalError(err))
			for _, finding := range all {
				finding.Line, finding.Column = max(1, finding.Line), max(1, finding.Column)
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

func (p parsedDocument) analyzerInput(path, configPath string, content []byte, units []scriptUnit, facts factAvailability) analyzer.Input {
	workflow := p.workflow
	if p.metadata != nil && workflow != nil {
		workflow = actionFactsWorkflow(p.metadata)
	}
	return documentAnalyzerInput(path, configPath, content, workflow, p.outline, units, facts)
}
