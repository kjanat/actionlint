package composition

import (
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/builtin"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// A standalone manifest has no workflow caller or supplied inputs. Validate its
// declarations directly, then use the same composite script lifecycle as uses:.
func visitStandaloneAction(meta *actionmeta.Metadata, rules []workflowvisit.Rule, analysis *compositeAnalyzer) error {
	deployment, err := scriptexecution.NewDeployment(analysis.sourceSnapshot, meta.Path(), meta.SourceMapped)
	if err != nil {
		return err
	}
	workflow := &workflowlang.Workflow{}
	analysis.beginWorkflow(workflow)
	analysis.scriptScope.Deployment = deployment
	execution.ActionOrigin(&analysis.scriptScope.Paths, analysis.context, deployment.ActionPath())
	var scripts []workflowvisit.Rule
	for _, rule := range rules {
		if err := rule.VisitWorkflowPre(workflow); err != nil {
			return err
		}
		if participant, ok := rule.(builtin.MetadataParticipant); ok {
			participant.CheckStandaloneMetadata(meta)
		}
		if participant, ok := rule.(builtin.CompositeParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	if strings.EqualFold(meta.Runs.Using, "composite") {
		if err := analysis.visitMetadataScripts(meta, analysis.invocations.RootContext(), scripts); err != nil {
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
