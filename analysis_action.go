package actionlint

import (
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

func isActionInput(path string) bool {
	directory := filepath.Dir(path)
	if filepath.Base(directory) == "workflows" && filepath.Base(filepath.Dir(directory)) == ".github" {
		return false
	}
	name := filepath.Base(path)
	return name == "action.yml" || name == "action.yaml"
}

func standaloneActionRule(name string) bool {
	switch name {
	case "action", "shellcheck", "ruff", "executable-bit", "deprecated-commands":
		return true
	default:
		return false
	}
}

// A standalone manifest has no workflow caller or supplied inputs. Validate its
// declarations directly, then use the same composite script lifecycle as uses:.
func visitStandaloneAction(meta *ActionMetadata, rules []Rule, analysis *compositeAnalyzer) error {
	workflow := &Workflow{}
	analysis.beginWorkflow(workflow)
	var scripts []Rule
	for _, rule := range rules {
		if err := rule.VisitWorkflowPre(workflow); err != nil && !analysis.deferWorkflowError(rule, err) {
			return err
		}
		if action, ok := rule.(*RuleAction); ok {
			action.standalone = meta
			action.checkLocalActionMetadata(meta, &ExecAction{Uses: &String{Pos: &Pos{Line: 1, Col: 1}}})
			action.standalone = nil
		} else if standaloneActionRule(rule.Name()) {
			scripts = append(scripts, rule)
		}
	}
	if strings.EqualFold(meta.Runs.Using, "composite") {
		// The manifest is provided explicitly; do not load it again or infer a
		// caller's checkout from its location on the analysis machine.
		call := &Step{Pos: &Pos{Line: 1, Col: 1}, Exec: &ExecAction{}}
		if err := analysis.visitMetadataScripts(meta, call, scripts, make(map[string]bool)); err != nil {
			return err
		}
	}
	if err := analysis.validateWorkflow(); err != nil {
		return err
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
func (rule *RuleAction) metadataFieldPosition(fallback *Pos, fields ...string) *Pos {
	if rule.standalone == nil {
		return fallback
	}
	node := actionSchemaNode(rule.standalone.schemaRoot)
	pos := &Pos{Line: 1, Col: 1}
	for _, field := range fields {
		if node == nil || node.Kind != yaml.MappingNode {
			break
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if strings.EqualFold(actionMetadataKey(key), field) {
				pos = &Pos{Line: key.Line, Col: key.Column}
				next = actionSchemaNode(node.Content[i+1])
				break
			}
		}
		node = next
	}
	return pos
}
