package workflowvisit

import "actionlint.kjanat.dev/v2/internal/results/finding"

// Findings takes ownership of reported findings and gives readers detached copies.
type Findings struct {
	items []*finding.Finding
}

func (f *Findings) Report(findings ...*finding.Finding) {
	f.items = append(f.items, findings...)
}

func (f *Findings) Errs() []*finding.Finding {
	out := make([]*finding.Finding, 0, len(f.items))
	for _, finding := range f.items {
		out = append(out, finding.Clone())
	}
	return out
}

func (f *Findings) FindingCount() int { return len(f.items) }

// AnnotateSince changes the findings recorded after mark, in recording order.
func (f *Findings) AnnotateSince(mark int, annotate func(*finding.Finding)) {
	for _, finding := range f.items[min(mark, len(f.items)):] {
		annotate(finding)
	}
}
