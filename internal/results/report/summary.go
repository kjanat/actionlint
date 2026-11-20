package report

import (
	"cmp"
	"slices"

	"actionlint.kjanat.dev/v2/internal/results"
)

// CheckSummary counts selected inputs and admitted, unsuppressed diagnostics,
// and identifies checks that could not finish.
type CheckSummary struct {
	IncompleteChecks   []IncompleteCheck  `json:"incomplete_checks,omitempty"`
	Files              int                `json:"files"`
	Findings           int                `json:"findings"`
	ProjectFindings    int                `json:"project_findings"`
	DependencyFindings int                `json:"dependency_findings"`
	ByFile             []FileFindingCount `json:"by_file,omitempty"`
	DependencyByFile   []FileFindingCount `json:"dependency_by_file,omitempty"`
	ByDiagnostic       []DiagnosticCount  `json:"by_diagnostic,omitempty"`
}

// FileFindingCount counts findings at their displayed source path.
type FileFindingCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// DiagnosticCount counts findings for one rule and optional diagnostic code.
type DiagnosticCount struct {
	Origin results.DiagnosticOrigin `json:"origin,omitempty"`
	Rule   string                   `json:"rule"`
	Code   string                   `json:"code,omitempty"`
	Count  int                      `json:"count"`
}

// NewCheckSummary groups findings and copies incomplete-check records. A finding
// can point into a referenced action or script rather than a selected file.
func NewCheckSummary(analysis Analysis) CheckSummary {
	summary := CheckSummary{Files: analysis.FileCount, Findings: len(analysis.Diagnostics), IncompleteChecks: slices.Clone(analysis.IncompleteChecks)}
	byFile := make(map[string]int)
	byDependency := make(map[string]int)
	type diagnosticKey struct {
		rule, code string
		origin     results.DiagnosticOrigin
	}
	byDiagnostic := make(map[diagnosticKey]int)
	for _, d := range analysis.Diagnostics {
		origin := results.DiagnosticOrigin("")
		if d.Origin == results.DependencyOrigin {
			summary.DependencyFindings++
			byDependency[d.Path]++
			origin = results.DependencyOrigin
		} else {
			summary.ProjectFindings++
			byFile[d.Path]++
		}
		code := d.Code
		if code == d.Rule {
			code = ""
		}
		byDiagnostic[diagnosticKey{d.Rule, code, origin}]++
	}
	for path, count := range byFile {
		summary.ByFile = append(summary.ByFile, FileFindingCount{Path: path, Count: count})
	}
	for key, count := range byDiagnostic {
		summary.ByDiagnostic = append(summary.ByDiagnostic, DiagnosticCount{Rule: key.rule, Code: key.code, Origin: key.origin, Count: count})
	}
	for path, count := range byDependency {
		summary.DependencyByFile = append(summary.DependencyByFile, FileFindingCount{Path: path, Count: count})
	}
	compareFiles := func(a, b FileFindingCount) int {
		if order := cmp.Compare(b.Count, a.Count); order != 0 {
			return order
		}
		return cmp.Compare(a.Path, b.Path)
	}
	slices.SortFunc(summary.ByFile, compareFiles)
	slices.SortFunc(summary.DependencyByFile, compareFiles)
	slices.SortFunc(summary.ByDiagnostic, func(a, b DiagnosticCount) int {
		if order := cmp.Compare(b.Count, a.Count); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Rule, b.Rule); order != 0 {
			return order
		}
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Origin, b.Origin))
	})
	return summary
}
