package policy

import (
	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

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
		Description: "Remove unused suppression", Applicability: applicability.Safe,
		Revisions: map[string]string{path: ref.Revision}, Edits: []results.Edit{edit},
	}, true
}
