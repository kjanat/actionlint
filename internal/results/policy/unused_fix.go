package policy

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

// UnusedSuppressionFix removes selectors after invocation-wide usage is known.
// The edit is range-checked and bound to the exact admitted document revision.
func (processor Processor) UnusedSuppressionFix(content []byte, path string, ref source.Reference, position results.Position, rules, allowed []string) (results.Fix, bool) {
	if path == "" || ref.Document == "" || ref.Revision != sourceid.Digest(content) {
		return results.Fix{}, false
	}
	edit, ok := suppression.Remove(content, position, rules, allowed, processor.RuleSet.Names())
	if !ok {
		return results.Fix{}, false
	}
	edit.Path = path
	return results.Fix{
		Description: "Remove unused suppression", Applicability: results.FixSafe,
		Revisions: map[string]string{path: ref.Revision}, Edits: []results.Edit{edit},
	}, true
}
