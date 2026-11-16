package policy

import (
	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

// unusedSuppressionFix removes selectors after invocation-wide usage is known.
// The edit is range-checked and bound to the exact admitted document revision.
func (processor Processor) unusedSuppressionFix(content []byte, path string, ref sourcetext.Reference, position sourcetext.Position, rules, allowed []string) (results.Fix, bool) {
	if path == "" || ref.Document == "" || ref.Revision != sha256hex.Digest(content) {
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
