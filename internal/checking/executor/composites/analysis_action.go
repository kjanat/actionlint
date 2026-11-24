package composites

import (
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func visitStandaloneAction(meta *actionmeta.Metadata, deployment scriptanalysis.Deployment, rules []workflowvisit.Rule, analysis *compositeAnalyzer) error {
	snapshot := analysis.sourceSnapshot
	if snapshot.Kind() != parsing.KindAction {
		return errors.New("action source snapshot is unavailable")
	}
	view, available := snapshot.ActionView()
	if !available {
		return errors.New("action declaration is unavailable")
	}
	original, hasOriginal := meta.Text()
	if !hasOriginal || original != string(snapshot.Content()) || meta.Path() != deployment.SourcePath() || meta.SourceMapped != (deployment.ActionPath() == "") {
		return fmt.Errorf("%w: action metadata provenance disagrees with source snapshot", scriptanalysis.ErrInvalidBatch)
	}
	if err := analysis.beginAction(deployment); err != nil {
		return err
	}
	files, err := localactions.InspectReferencedFiles(analysis.ctx, meta, analysis.reader)
	if err != nil {
		return err
	}
	if err := analysis.recordUncheckedFiles(meta, files, snapshot.Reference(), analysis.invocations.RootContext()); err != nil {
		return err
	}
	var scripts []workflowvisit.InvocationParticipant
	for _, rule := range rules {
		if participant, ok := rule.(workflowvisit.MetadataParticipant); ok {
			if err := participant.CheckStandaloneMetadata(meta, snapshot, files); err != nil {
				return err
			}
		}
		if participant, ok := rule.(workflowvisit.InvocationParticipant); ok {
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
