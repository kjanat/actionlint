package checking

import (
	"slices"
	"sync"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type suppressionUse struct {
	document, revision, rule string
	position                 sourcepos.Position
}

type checkRule struct{ document, revision, rule string }

// addUnusedSuppressionFixes shares one edit across all unused selectors in a
// comment. This keeps fix-all transactions disjoint and retains selectors used
// by another invocation of the same action or workflow.
func addUnusedSuppressionFixes(files []analyzedFile, allowed []string) {
	type comment struct {
		document, revision string
		position           sourcepos.Position
	}
	type group struct {
		content  []byte
		path     string
		rules    []string
		findings []*findingmodel.Finding
	}
	groups := make(map[comment]*group)
	for index := range files {
		file := &files[index]
		for _, finding := range file.errors {
			ref := finding.Reference
			if finding.UnusedSuppressionRule == "" || ref == nil {
				continue
			}
			key := comment{ref.Document, ref.Revision, finding.Start}
			entry, exists := groups[key]
			if !exists {
				content := finding.Source
				if content == nil {
					content = file.source.Content
				}
				path := finding.Path
				if path == "" {
					path = file.source.Path
				}
				entry = &group{content: content, path: path}
				groups[key] = entry
			}
			if !slices.Contains(entry.rules, finding.UnusedSuppressionRule) {
				entry.rules = append(entry.rules, finding.UnusedSuppressionRule)
			}
			entry.findings = append(entry.findings, finding)
		}
	}
	for key, entry := range groups {
		fix, ok := (policy.Processor{RuleSet: ConfigurationRules()}).UnusedSuppressionFix(entry.content, entry.path,
			sourcepos.Reference{Document: key.document, Revision: key.revision}, key.position, entry.rules, allowed)
		if ok {
			for _, finding := range entry.findings {
				finding.Fixes = []results.Fix{fix}
			}
		}
	}
}

// suppressionUses joins usage and incomplete analyses across callers without
// mixing documents or revisions. Absence of findings proves a selector unused
// only when every invocation of that source completed the relevant check.
type suppressionUses struct {
	mu   sync.Mutex
	seen map[suppressionUse]bool
}

func (u *suppressionUses) observe(ref sourcepos.Reference, position sourcepos.Position, rule string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.seen == nil {
		u.seen = make(map[suppressionUse]bool)
	}
	u.seen[suppressionUse{ref.Document, ref.Revision, rule, position}] = true
}

func (u *suppressionUses) discardUnproven(files []analyzedFile, checks []report.IncompleteCheck) {
	u.mu.Lock()
	defer u.mu.Unlock()
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
		files[index].errors = slices.DeleteFunc(files[index].errors, func(finding *findingmodel.Finding) bool {
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
