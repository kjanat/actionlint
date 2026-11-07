package composites

import (
	"fmt"
	"slices"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	"actionlint.kjanat.dev/v2/internal/input/source"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// recordUncheckedFiles records only the metadata checks that consume these
// facts. Other checks of the same source can finish and report their findings.
func (analysis *compositeAnalyzer) recordUncheckedFiles(meta *actionmeta.Metadata, files actionmeta.ReferencedFiles, source source.Reference, context invocation.Context) {
	if meta == nil {
		return
	}
	var unknown []string
	for _, path := range meta.Runs.FileReferences() {
		if files.Lookup(path) == actionmeta.FileUnknown {
			unknown = append(unknown, path)
		}
	}
	if len(unknown) == 0 {
		return
	}
	slices.Sort(unknown)
	unknown = slices.Compact(unknown)
	reason := fmt.Sprintf("could not determine availability of action execution files %q", unknown)
	entry := analysis.invocations.RootContext().Site()
	for _, pass := range analysis.passes {
		if participant, consumesFiles := pass.(checkcalls.MetadataParticipant); consumesFiles {
			analysis.incompleteChecks = append(analysis.incompleteChecks, report.IncompleteCheck{Source: source, Invocation: report.CheckInvocation{Entry: entry, ID: context.ID()}, Rule: participant.Name(), Reason: reason})
		}
	}
}
