//go:build conformance

package conformance_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"os"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/conformance"
)

var upstreamAssessments = []string{"actionlint-gap", "reference-gap", "extra-lint", "adapter-limit", "dependency-gap", "contract-difference", "unassessed"}

type upstreamFinding struct {
	Category    string   `json:"category"`
	Scope       string   `json:"scope"`
	Diagnostics []int    `json:"diagnostic_indices,omitempty"`
	Rationale   string   `json:"rationale"`
	Evidence    []string `json:"evidence,omitempty"`
}

type upstreamAssessmentGroup struct {
	Cases    map[string]string `json:"cases"`
	Findings []upstreamFinding `json:"findings"`
}

type upstreamReview struct {
	CaseID                 string            `json:"case_id"`
	ObservationFingerprint string            `json:"observation_fingerprint"`
	ReviewedFingerprint    string            `json:"reviewed_fingerprint,omitempty"`
	Status                 string            `json:"status"`
	Findings               []upstreamFinding `json:"findings,omitempty"`
}

type upstreamScore struct {
	Cases            int            `json:"cases"`
	Agreements       int            `json:"agreements"`
	Differences      int            `json:"differences"`
	New              int            `json:"new_differences"`
	Changed          int            `json:"changed_diagnostics"`
	Resolved         int            `json:"resolved_differences"`
	Current          int            `json:"current_assessments"`
	Stale            int            `json:"stale_assessments"`
	Missing          int            `json:"missing_assessments"`
	CategoryCases    map[string]int `json:"cases_by_category"`
	CategoryFindings map[string]int `json:"findings_by_category"`
}

type upstreamAssessmentReport struct {
	Score   upstreamScore    `json:"score"`
	Reviews []upstreamReview `json:"reviews"`
}

func assessUpstreamResults(results []upstreamResult, baseline map[string][]string, groups []upstreamAssessmentGroup) (upstreamAssessmentReport, error) {
	report := upstreamAssessmentReport{Score: upstreamScore{
		Cases: len(results), CategoryCases: map[string]int{}, CategoryFindings: map[string]int{},
	}, Reviews: []upstreamReview{}}
	for _, category := range upstreamAssessments {
		report.Score.CategoryCases[category] = 0
		report.Score.CategoryFindings[category] = 0
	}
	byID := map[string]upstreamAssessmentGroup{}
	for _, group := range groups {
		if len(group.Cases) == 0 || len(group.Findings) == 0 {
			return report, errors.New("assessment requires cases and findings")
		}
		for _, finding := range group.Findings {
			if !slices.Contains(upstreamAssessments, finding.Category) || strings.TrimSpace(finding.Rationale) == "" {
				return report, fmt.Errorf("invalid assessment category or empty rationale: %q", finding.Category)
			}
			if !slices.Contains([]string{"fixture", "feature", "diagnostic"}, finding.Scope) ||
				(finding.Scope == "diagnostic") != (len(finding.Diagnostics) > 0) {
				return report, fmt.Errorf("invalid assessment scope or diagnostic selection: %q", finding.Scope)
			}
			if finding.Category != "unassessed" && len(finding.Evidence) == 0 {
				return report, fmt.Errorf("assessment %q requires evidence", finding.Category)
			}
			for _, evidence := range finding.Evidence {
				if strings.TrimSpace(evidence) == "" {
					return report, errors.New("assessment evidence must not be empty")
				}
			}
		}
		for id, fingerprint := range group.Cases {
			decoded, err := hex.DecodeString(fingerprint)
			if err != nil || len(decoded) != 32 {
				return report, fmt.Errorf("invalid observation fingerprint for %q", id)
			}
			if _, duplicate := byID[id]; duplicate {
				return report, fmt.Errorf("duplicate assessment for %q", id)
			}
			byID[id] = group
		}
	}
	for _, result := range results {
		group, assessed := byID[result.ID]
		delete(byID, result.ID)
		review := upstreamReview{
			CaseID: result.ID, ObservationFingerprint: result.Fingerprint,
			ReviewedFingerprint: group.Cases[result.ID], Findings: group.Findings,
		}
		recorded, known := baseline[result.ID]
		if !result.Difference {
			report.Score.Agreements++
			if known {
				report.Score.Resolved++
			}
			if assessed {
				review.Status = "resolved"
				report.Reviews = append(report.Reviews, review)
			}
			continue
		}
		report.Score.Differences++
		if !known {
			report.Score.New++
		} else if !slices.Equal(recorded, result.Actual) {
			report.Score.Changed++
		}
		switch {
		case !assessed:
			review.Status = "missing"
			report.Score.Missing++
		case group.Cases[result.ID] != result.Fingerprint:
			review.Status = "stale"
			report.Score.Stale++
		default:
			review.Status = "current"
			report.Score.Current++
			categories := map[string]bool{}
			for _, finding := range group.Findings {
				indices := map[int]bool{}
				for _, index := range finding.Diagnostics {
					if index < 0 || index >= len(result.Actual) || indices[index] {
						return report, fmt.Errorf("invalid or duplicate diagnostic index %d for %q", index, result.ID)
					}
					indices[index] = true
				}
				report.Score.CategoryFindings[finding.Category]++
				categories[finding.Category] = true
			}
			for category := range categories {
				report.Score.CategoryCases[category]++
			}
		}
		report.Reviews = append(report.Reviews, review)
	}
	if len(byID) != 0 {
		return report, fmt.Errorf("assessment refers to missing observations: %v", byID)
	}
	return report, nil
}

func upstreamAssessmentInventory(groups []upstreamAssessmentGroup, report upstreamAssessmentReport) string {
	statuses := make(map[string]string, len(report.Reviews))
	for _, review := range report.Reviews {
		statuses[review.CaseID] = review.Status
	}
	categoryGroups := map[string]int{}
	for _, group := range groups {
		categories := map[string]bool{}
		for _, finding := range group.Findings {
			categories[finding.Category] = true
		}
		for category := range categories {
			categoryGroups[category]++
		}
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Assessment inventory: %d groups; %d cases without a review\n", len(groups), report.Score.Missing)
	for _, category := range upstreamAssessments {
		fmt.Fprintf(&text, "  %s: %d groups, %d current cases, %d current findings\n", category, categoryGroups[category], report.Score.CategoryCases[category], report.Score.CategoryFindings[category])
	}
	text.WriteString("\nGroup indices refer to assessments.json; group counts include stale and resolved reviews.\n")
	for index, group := range groups {
		counts := map[string]int{}
		for id := range group.Cases {
			counts[statuses[id]]++
		}
		for _, finding := range group.Findings {
			scope := finding.Scope
			if len(finding.Diagnostics) != 0 {
				scope += fmt.Sprintf(" %v", finding.Diagnostics)
			}
			fmt.Fprintf(&text, "[%d] %s / %s: cases=%d (current=%d stale=%d resolved=%d) — %s\n", index, finding.Category, scope, len(group.Cases), counts["current"], counts["stale"], counts["resolved"], strings.Join(strings.Fields(finding.Rationale), " "))
		}
	}
	for _, review := range report.Reviews {
		if review.Status == "missing" {
			fmt.Fprintf(&text, "Missing review: %s\n", review.CaseID)
		}
	}
	return text.String()
}

func writeUpstreamSummary(t *testing.T, filename string, sources []conformance.Source, results []upstreamResult, baseline map[string][]string, groups []upstreamAssessmentGroup) {
	t.Helper()
	report, err := assessUpstreamResults(results, baseline, groups)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	text.WriteString("## Reference observations\n\n| Source | Cases | Agreements | Differences |\n| --- | ---: | ---: | ---: |\n")
	for _, source := range sources {
		var cases, differences int
		for _, result := range results {
			if strings.HasPrefix(result.ID, source.Name+"/") {
				cases++
				if result.Difference {
					differences++
				}
			}
		}
		fmt.Fprintf(&text, "| [%s](https://github.com/%s/tree/%s) | %d | %d | %d |\n", source.Name, source.Repository, source.Revision, cases, cases-differences, differences)
	}
	fmt.Fprintf(&text, "\nNew differences: %d. Changed diagnostics: %d. Resolved differences: %d.\n", report.Score.New, report.Score.Changed, report.Score.Resolved)
	text.WriteString("\n## Reviewed assessments\n\nThese judgments are stored separately from observations. Only reviews matching the observation fingerprint contribute to the category counts. Cases can have multiple findings and categories; category columns are not additive fixture totals.\n\n")
	fmt.Fprintf(&text, "Current: %d. Stale: %d. Missing: %d.\n\n", report.Score.Current, report.Score.Stale, report.Score.Missing)
	text.WriteString("| Category | Distinct cases | Findings |\n| --- | ---: | ---: |\n")
	for _, category := range upstreamAssessments {
		fmt.Fprintf(&text, "| %s | %d | %d |\n", category, report.Score.CategoryCases[category], report.Score.CategoryFindings[category])
	}
	fmt.Fprintf(&text, "\n<details>\n<summary>Assessment inventory (%d groups)</summary>\n\n<pre>%s</pre>\n</details>\n", len(groups), html.EscapeString(upstreamAssessmentInventory(groups, report)))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(text.String())
	closeErr := f.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
