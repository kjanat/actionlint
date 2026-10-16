package engine

import (
	"path/filepath"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func isActionInput(path string) bool {
	directory := filepath.Dir(path)
	if filepath.Base(directory) == "workflows" && filepath.Base(filepath.Dir(directory)) == ".github" {
		return false
	}
	name := filepath.Base(path)
	return name == "action.yml" || name == "action.yaml"
}

// A standalone manifest has no workflow caller or supplied inputs. Validate its
// declarations directly, then use the same composite script lifecycle as uses:.
func visitStandaloneAction(meta *ActionMetadata, rules []Rule, analysis *compositeAnalyzer) error {
	workflow := &workflowlang.Workflow{}
	analysis.beginWorkflow(workflow)
	var scripts []Rule
	for _, rule := range rules {
		if err := rule.VisitWorkflowPre(workflow); err != nil {
			return err
		}
		if participant, ok := rule.(metadataParticipant); ok {
			participant.checkStandaloneMetadata(meta)
		}
		if participant, ok := rule.(compositeParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	if strings.EqualFold(meta.Runs.Using, "composite") {
		// The manifest is provided explicitly; do not load it again or infer a
		// caller's checkout from its location on the analysis machine.
		call := &workflowlang.Step{Pos: &workflowlang.Pos{Line: 1, Col: 1}, Exec: &workflowlang.ExecAction{}}
		if err := analysis.visitMetadataScripts(meta, call, scripts, make(map[string]bool)); err != nil {
			return err
		}
	}
	for _, rule := range rules {
		if err := rule.VisitWorkflowPost(workflow); err != nil {
			return err
		}
	}
	return nil
}

// Missing fields point at their nearest declared parent. Existing fields point
// at their key. Workflow-call diagnostics retain their original uses: position.
func (rule *RuleAction) metadataFieldPosition(fallback *workflowlang.Pos, fields ...string) *workflowlang.Pos {
	if rule.standalone == nil {
		return fallback
	}
	node := actionSchemaNode(rule.standalone.schemaRoot)
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	for _, field := range fields {
		if node == nil || node.Kind != yaml.MappingNode {
			break
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if strings.EqualFold(actionMetadataKey(key), field) {
				pos = &workflowlang.Pos{Line: key.Line, Col: key.Column}
				next = actionSchemaNode(node.Content[i+1])
				break
			}
		}
		node = next
	}
	return pos
}
