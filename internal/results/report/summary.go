package report

import (
	"cmp"
	"slices"
)

// CheckSummary counts selected inputs and admitted, unsuppressed diagnostics,
// and identifies checks that could not finish.
type CheckSummary struct {
	IncompleteChecks []IncompleteCheck  `json:"incomplete_checks,omitempty"`
	Files            int                `json:"files"`
	Findings         int                `json:"findings"`
	ByFile           []FileFindingCount `json:"by_file,omitempty"`
	ByDiagnostic     []DiagnosticCount  `json:"by_diagnostic,omitempty"`
}

// FileFindingCount counts findings at their displayed source path.
type FileFindingCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// DiagnosticCount counts findings for one rule and optional diagnostic code.
type DiagnosticCount struct {
	Rule  string `json:"rule"`
	Code  string `json:"code,omitempty"`
	Count int    `json:"count"`
}

// NewCheckSummary groups findings and copies incomplete-check records. A finding
// can point into a referenced action or script rather than a selected file.
func NewCheckSummary(analysis Analysis) CheckSummary {
	summary := CheckSummary{Files: analysis.FileCount, Findings: len(analysis.Diagnostics), IncompleteChecks: slices.Clone(analysis.IncompleteChecks)}
	byFile := make(map[string]int)
	type diagnosticKey struct{ rule, code string }
	byDiagnostic := make(map[diagnosticKey]int)
	for _, d := range analysis.Diagnostics {
		byFile[d.Path]++
		code := d.Code
		if code == d.Rule {
			code = ""
		}
		byDiagnostic[diagnosticKey{d.Rule, code}]++
	}
	for path, count := range byFile {
		summary.ByFile = append(summary.ByFile, FileFindingCount{Path: path, Count: count})
	}
	for key, count := range byDiagnostic {
		summary.ByDiagnostic = append(summary.ByDiagnostic, DiagnosticCount{Rule: key.rule, Code: key.code, Count: count})
	}
	slices.SortFunc(summary.ByFile, func(a, b FileFindingCount) int {
		if order := cmp.Compare(b.Count, a.Count); order != 0 {
			return order
		}
		return cmp.Compare(a.Path, b.Path)
	})
	slices.SortFunc(summary.ByDiagnostic, func(a, b DiagnosticCount) int {
		if order := cmp.Compare(b.Count, a.Count); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Rule, b.Rule); order != 0 {
			return order
		}
		return cmp.Compare(a.Code, b.Code)
	})
	return summary
}
