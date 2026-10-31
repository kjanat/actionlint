package visit

import findingmodel "actionlint.kjanat.dev/v2/internal/finding"

// Findings accumulates the diagnostics a rule reports. Reporting a finding
// transfers it to the accumulator: readers receive detached copies, and
// recorded findings change only through AnnotateSince. The zero value is empty.
type Findings struct {
	items []*findingmodel.Finding
}

// Report records findings and takes ownership of them.
func (f *Findings) Report(findings ...*findingmodel.Finding) {
	f.items = append(f.items, findings...)
}

// Errs returns copies of the recorded findings, detached from the accumulator.
func (f *Findings) Errs() []*findingmodel.Finding {
	out := make([]*findingmodel.Finding, 0, len(f.items))
	for _, finding := range f.items {
		out = append(out, finding.Clone())
	}
	return out
}

// FindingCount returns the number of recorded findings. It marks the current
// position for AnnotateSince.
func (f *Findings) FindingCount() int { return len(f.items) }

// AnnotateSince applies annotate, in recording order, to every finding recorded
// after mark, a prior FindingCount. Annotation changes the recorded findings.
func (f *Findings) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
	for _, finding := range f.items[min(mark, len(f.items)):] {
		annotate(finding)
	}
}
