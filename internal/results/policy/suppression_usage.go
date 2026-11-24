package policy

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type suppressionUse struct {
	document, revision, rule string
	position                 sourcetext.Position
}

type checkRevision struct{ document, revision string }

type checkRule struct{ document, revision, rule string }

func addUnusedSuppressionFixes(files []evaluatedBatch) {
	type comment struct {
		document, revision string
		position           sourcetext.Position
	}
	type group struct {
		content   []byte
		path      string
		rules     []string
		findings  []*finding.Finding
		processor Processor
		allowed   []string
	}
	groups := make(map[comment]*group)
	for index := range files {
		file := &files[index]
		for _, finding := range file.Findings {
			ref := finding.Reference
			if finding.UnusedSuppressionRule == "" || ref == nil {
				continue
			}
			key := comment{ref.Document, ref.Revision, finding.Start}
			entry, exists := groups[key]
			if !exists {
				content := finding.Source.Content
				if content == nil {
					content = file.Policy.Content
				}
				path := finding.Path
				if path == "" {
					path = file.InputPath
				}
				entry = &group{content: content, path: path, processor: file.Processor, allowed: file.Processor.SuppressionRules(file.Policy.AdditionalRules...)}
				groups[key] = entry
			}
			if !slices.Contains(entry.rules, finding.UnusedSuppressionRule) {
				entry.rules = append(entry.rules, finding.UnusedSuppressionRule)
			}
			entry.findings = append(entry.findings, finding)
		}
	}
	for key, entry := range groups {
		fix, ok := entry.processor.unusedSuppressionFix(entry.content, entry.path,
			sourcetext.Reference{Document: key.document, Revision: key.revision}, key.position, entry.rules, entry.allowed)
		if ok {
			for _, finding := range entry.findings {
				finding.Fixes = []results.Fix{fix}
			}
		}
	}
}

// Absence of findings proves a selector unused only when every invocation of that source completed the relevant check.
type suppressionUses struct {
	seen map[suppressionUse]bool
}

func (u *suppressionUses) observe(ref sourcetext.Reference, position sourcetext.Position, rule string) {
	if u.seen == nil {
		u.seen = make(map[suppressionUse]bool)
	}
	u.seen[suppressionUse{ref.Document, ref.Revision, rule, position}] = true
}

func (u *suppressionUses) discardUnproven(files []evaluatedBatch, checks []report.IncompleteCheck) {
	incomplete := make(map[checkRevision]bool)
	unavailable := make(map[checkRule]bool)
	for _, check := range checks {
		if check.Rule == "" {
			incomplete[checkRevision{check.Source.Document, check.Source.Revision}] = true
		} else {
			unavailable[checkRule{check.Source.Document, check.Source.Revision, check.Rule}] = true
		}
	}
	for index := range files {
		files[index].Findings = slices.DeleteFunc(files[index].Findings, func(finding *finding.Finding) bool {
			ref := finding.Reference
			if finding.UnusedSuppressionRule == "" || ref == nil {
				return false
			}
			skipped := unavailable[checkRule{ref.Document, ref.Revision, finding.UnusedSuppressionRule}]
			return (incomplete[checkRevision{ref.Document, ref.Revision}] ||
				skipped ||
				u.seen[suppressionUse{ref.Document, ref.Revision, finding.UnusedSuppressionRule, finding.Start}])
		})
	}
}
