package diagnosticpolicy

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/internal/suppression"
	"actionlint.kjanat.dev/v2/source"
)

// UnusedSuppressionFix removes selectors after invocation-wide usage is known.
// The edit is range-checked and bound to the exact admitted document revision.
func UnusedSuppressionFix(content []byte, path string, ref source.Reference, position diagnostic.Position, rules, allowed []string) (diagnostic.Fix, bool) {
	if path == "" || ref.Document == "" || ref.Revision != sourceid.Digest(content) {
		return diagnostic.Fix{}, false
	}
	edit, ok := suppression.Remove(content, position, rules, allowed)
	if !ok {
		return diagnostic.Fix{}, false
	}
	edit.Path = path
	return diagnostic.Fix{
		Description: "Remove unused suppression", Applicability: diagnostic.FixSafe,
		Revisions: map[string]string{path: ref.Revision}, Edits: []diagnostic.Edit{edit},
	}, true
}
