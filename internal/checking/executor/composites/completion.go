package composites

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// recordUncheckedFiles records only the metadata checks that consume these
// facts. Other checks of the same source can finish and report their findings.
func (analysis *compositeAnalyzer) recordUncheckedFiles(meta *actionmeta.Metadata, files actionmeta.ReferencedFiles, source source.Reference, context invocation.Context) error {
	if meta == nil {
		return nil
	}
	var unknown []string
	for _, path := range meta.Runs.FileReferences() {
		if files.Lookup(path) == actionmeta.FileUnknown {
			unknown = append(unknown, path)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	bindings := analysis.bindings
	if analysis.bindSource != nil {
		var err error
		bindings, err = analysis.bindSource(bindings, source.Document)
		if err != nil {
			return err
		}
	}
	slices.Sort(unknown)
	unknown = slices.Compact(unknown)
	reason := fmt.Sprintf("could not determine availability of action execution files %q", unknown)
	entry := analysis.invocations.RootContext().Site()
	for _, pass := range analysis.passes {
		if participant, consumesFiles := pass.(workflowvisit.MetadataParticipant); consumesFiles && bindings.DiagnosticLevel(participant.Name()) != "off" {
			analysis.incompleteChecks = append(analysis.incompleteChecks, report.IncompleteCheck{Source: source, Invocation: report.CheckInvocation{Entry: entry, ID: context.ID()}, Rule: participant.Name(), Reason: reason})
		}
	}
	return nil
}
