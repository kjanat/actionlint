package composition

import (
	"errors"
	"fmt"
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// A standalone manifest has no workflow caller or supplied inputs. Validate its
// declarations directly, then use the same composite script lifecycle as uses:.
func visitStandaloneAction(meta *actionmeta.Metadata, deployment scriptexecution.Deployment, rules []workflowvisit.Rule, analysis *compositeAnalyzer) error {
	snapshot := analysis.sourceSnapshot
	if snapshot == nil || snapshot.Kind() != documentparse.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	view, available := snapshot.ActionView()
	if !available {
		return errors.New("action declaration is unavailable")
	}
	original, hasOriginal := meta.OriginalSource()
	if !hasOriginal || original != string(snapshot.Content()) || meta.Path() != deployment.SourcePath() || meta.SourceMapped != (deployment.ActionPath() == "") {
		return fmt.Errorf("%w: action metadata provenance disagrees with source snapshot", scriptexecution.ErrInvalidBatch)
	}
	if err := analysis.beginAction(deployment); err != nil {
		return err
	}
	var scripts []builtin.InvocationParticipant
	for _, rule := range rules {
		if participant, ok := rule.(builtin.MetadataParticipant); ok {
			if err := participant.CheckStandaloneMetadata(meta, snapshot); err != nil {
				return err
			}
		}
		if participant, ok := rule.(builtin.InvocationParticipant); ok {
			scripts = append(scripts, participant)
		}
	}
	if strings.EqualFold(view.Using(), "composite") {
		if err := analysis.visitMetadataScripts(meta, snapshot, deployment.Address(), analysis.invocations.RootContext(), scripts); err != nil {
			return err
		}
	}
	return nil
}
