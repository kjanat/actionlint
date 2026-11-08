package composites

import (
	"errors"
	"fmt"
	"strings"

	actionfiles "actionlint.kjanat.dev/v2/internal/input/references/actions/files"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// visitStandaloneAction validates a manifest without a workflow caller or supplied
// inputs. For composite actions, it also visits the action's steps.
func visitStandaloneAction(meta *actionmeta.Metadata, deployment scriptexecution.Deployment, rules []workflowvisit.Rule, analysis *compositeAnalyzer) error {
	snapshot := analysis.sourceSnapshot
	if snapshot.Kind() != parsing.KindAction {
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
	files, err := actionfiles.Inspect(analysis.ctx, meta, analysis.reader)
	if err != nil {
		return err
	}
	analysis.recordUncheckedFiles(meta, files, snapshot.Reference(), analysis.invocations.RootContext())
	var scripts []checkcalls.InvocationParticipant
	for _, rule := range rules {
		if participant, ok := rule.(checkcalls.MetadataParticipant); ok {
			if err := participant.CheckStandaloneMetadata(meta, snapshot, files); err != nil {
				return err
			}
		}
		if participant, ok := rule.(checkcalls.InvocationParticipant); ok {
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
