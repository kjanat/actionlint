package engine

import (
	"path/filepath"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
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
func visitStandaloneAction(meta *actionmeta.Metadata, rules []workflowvisit.Rule, analysis *compositeAnalyzer) error {
	workflow := &workflowlang.Workflow{}
	analysis.beginWorkflow(workflow)
	var scripts []workflowvisit.Rule
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
