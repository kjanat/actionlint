//go:build conformance

package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"actionlint.kjanat.dev/internal/conformance"
	"github.com/google/go-cmp/cmp"
)

func reviewedUpstreamCase(id string) (upstreamResult, upstreamAssessmentGroup) {
	result := upstreamResult{ID: id, Difference: true, Actual: []string{"diagnostic one", "diagnostic two"}}
	result.Fingerprint = upstreamFingerprint(conformance.Source{Name: "test"}, result)
	return result, upstreamAssessmentGroup{
		Cases:    map[string]string{id: result.Fingerprint},
		Findings: []upstreamFinding{{Category: "extra-lint", Scope: "diagnostic", Diagnostics: []int{0}, Rationale: "Additional lint finding", Evidence: []string{"fixture:" + id}}},
	}
}

func TestUpstreamConformanceScore(t *testing.T) {
	mixed, group := reviewedUpstreamCase("mixed")
	group.Findings = append(group.Findings,
		upstreamFinding{Category: "extra-lint", Scope: "diagnostic", Diagnostics: []int{1}, Rationale: "Another lint finding", Evidence: []string{"fixture:mixed"}},
		upstreamFinding{Category: "reference-gap", Scope: "feature", Rationale: "Runtime feature missing in reference", Evidence: []string{"recorded runtime probe"}},
	)
	stale, staleGroup := reviewedUpstreamCase("stale")
	old := append([]string(nil), stale.Actual...)
	stale.Actual = []string{"changed"}
	stale.Fingerprint = upstreamFingerprint(conformance.Source{Name: "test"}, stale)
	resolved, resolvedGroup := reviewedUpstreamCase("resolved")
	resolved.Difference = false
	resolved.Actual = []string{}
	resolved.Fingerprint = upstreamFingerprint(conformance.Source{Name: "test"}, resolved)
	results := []upstreamResult{mixed, stale, resolved, {ID: "agreement"}, {ID: "missing", Difference: true}, {ID: "new", Difference: true}}
	baseline := map[string][]string{"mixed": mixed.Actual, "stale": old, "resolved": old, "missing": {}}
	report, err := assessUpstreamResults(results, baseline, []upstreamAssessmentGroup{group, staleGroup, resolvedGroup})
	if err != nil {
		t.Fatal(err)
	}
	want := upstreamScore{Cases: 6, Agreements: 2, Differences: 4, New: 1, Changed: 1, Resolved: 1, Current: 1, Stale: 1, Missing: 2,
		CategoryCases: map[string]int{}, CategoryFindings: map[string]int{},
	}
	for _, category := range upstreamAssessments {
		want.CategoryCases[category] = 0
		want.CategoryFindings[category] = 0
	}
	want.CategoryCases["extra-lint"], want.CategoryFindings["extra-lint"] = 1, 2
	want.CategoryCases["reference-gap"], want.CategoryFindings["reference-gap"] = 1, 1
	if diff := cmp.Diff(want, report.Score); diff != "" {
		t.Fatalf("score (-want +got):\n%s", diff)
	}
	inventory := upstreamAssessmentInventory([]upstreamAssessmentGroup{group, staleGroup, resolvedGroup}, report)
	for _, want := range []string{
		"Assessment inventory: 3 groups; 2 cases without a review",
		"extra-lint: 3 groups, 1 current cases, 2 current findings",
		"reference-gap: 1 groups, 1 current cases, 1 current findings",
		"[0] reference-gap / feature: cases=1 (current=1 stale=0 resolved=0) — Runtime feature missing in reference",
		"[1] extra-lint / diagnostic [0]: cases=1 (current=0 stale=1 resolved=0)",
		"[2] extra-lint / diagnostic [0]: cases=1 (current=0 stale=0 resolved=1)",
		"Missing review: missing", "Missing review: new",
	} {
		if !strings.Contains(inventory, want) {
			t.Errorf("inventory missing %q:\n%s", want, inventory)
		}
	}
	var statuses []string
	for _, review := range report.Reviews {
		statuses = append(statuses, review.Status)
	}
	if diff := cmp.Diff([]string{"current", "stale", "resolved", "missing", "missing"}, statuses); diff != "" {
		t.Fatal(diff)
	}
}

func TestUpstreamConformanceObservationSeparation(t *testing.T) {
	result, group := reviewedUpstreamCase("case")
	observations := upstreamObservationReport{Observations: []upstreamResult{result}}
	before, err := json.Marshal(observations)
	if err != nil {
		t.Fatal(err)
	}
	baseline := map[string][]string{result.ID: result.Actual}
	first, err := assessUpstreamResults(observations.Observations, baseline, []upstreamAssessmentGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	group.Findings[0].Category = "actionlint-gap"
	second, err := assessUpstreamResults(observations.Observations, baseline, []upstreamAssessmentGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(observations)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("assessment changed observations")
	}
	if first.Score.CategoryCases["extra-lint"] != 1 || second.Score.CategoryCases["actionlint-gap"] != 1 {
		t.Fatal("assessment did not change derived score")
	}
	for _, forbidden := range []string{`"assessment"`, `"score"`, `"rationale"`, `"reason"`, `"findings"`} {
		if strings.Contains(string(after), forbidden) {
			t.Errorf("observation contains %s", forbidden)
		}
	}
}

func TestUpstreamConformanceAssessmentStaleness(t *testing.T) {
	for name, change := range map[string]func(*conformance.Source, *upstreamResult){
		"source revision": func(s *conformance.Source, _ *upstreamResult) { s.Revision = "new" },
		"input":           func(_ *conformance.Source, r *upstreamResult) { r.InputSHA256 = "new" },
		"diagnostics":     func(_ *conformance.Source, r *upstreamResult) { r.Actual = []string{"new"} },
		"expectation":     func(_ *conformance.Source, r *upstreamResult) { r.ExpectedReject = true },
		"reference skip":  func(_ *conformance.Source, r *upstreamResult) { r.ReferenceSkips = []string{"TypeScript"} },
		"contract":        func(_ *conformance.Source, r *upstreamResult) { r.Contract = "new" },
	} {
		t.Run(name, func(t *testing.T) {
			result, group := reviewedUpstreamCase("case")
			source := conformance.Source{Name: "test"}
			change(&source, &result)
			result.Fingerprint = upstreamFingerprint(source, result)
			report, err := assessUpstreamResults([]upstreamResult{result}, nil, []upstreamAssessmentGroup{group})
			if err != nil {
				t.Fatal(err)
			}
			if report.Score.Stale != 1 || report.Score.Current != 0 || report.Score.CategoryFindings["extra-lint"] != 0 {
				t.Fatalf("stale review counted: %+v", report)
			}
		})
	}
}

func TestUpstreamConformanceInvalidAssessments(t *testing.T) {
	for name, change := range map[string]func(*upstreamAssessmentGroup){
		"missing cases":       func(g *upstreamAssessmentGroup) { g.Cases = nil },
		"missing findings":    func(g *upstreamAssessmentGroup) { g.Findings = nil },
		"missing observation": func(g *upstreamAssessmentGroup) { g.Cases["unknown"] = g.Cases["case"] },
		"invalid fingerprint": func(g *upstreamAssessmentGroup) { g.Cases["case"] = "invalid" },
		"invalid category":    func(g *upstreamAssessmentGroup) { g.Findings[0].Category = "correct" },
		"empty rationale":     func(g *upstreamAssessmentGroup) { g.Findings[0].Rationale = " " },
		"missing evidence":    func(g *upstreamAssessmentGroup) { g.Findings[0].Evidence = nil },
		"empty evidence":      func(g *upstreamAssessmentGroup) { g.Findings[0].Evidence = []string{" "} },
		"invalid scope":       func(g *upstreamAssessmentGroup) { g.Findings[0].Scope = "all" },
		"indices on feature":  func(g *upstreamAssessmentGroup) { g.Findings[0].Scope = "feature" },
		"missing indices":     func(g *upstreamAssessmentGroup) { g.Findings[0].Diagnostics = nil },
		"negative index":      func(g *upstreamAssessmentGroup) { g.Findings[0].Diagnostics = []int{-1} },
		"out of bounds index": func(g *upstreamAssessmentGroup) { g.Findings[0].Diagnostics = []int{2} },
		"duplicate index":     func(g *upstreamAssessmentGroup) { g.Findings[0].Diagnostics = []int{0, 0} },
	} {
		t.Run(name, func(t *testing.T) {
			result, group := reviewedUpstreamCase("case")
			change(&group)
			if _, err := assessUpstreamResults([]upstreamResult{result}, nil, []upstreamAssessmentGroup{group}); err == nil {
				t.Fatal("invalid assessment accepted")
			}
		})
	}
	result, group := reviewedUpstreamCase("case")
	if _, err := assessUpstreamResults([]upstreamResult{result}, nil, []upstreamAssessmentGroup{group, group}); err == nil {
		t.Fatal("duplicate case assignment accepted")
	}
}

func TestUpstreamConformanceScoreSummary(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "summary.md")
	result, group := reviewedUpstreamCase("languageservices/insert")
	group.Findings[0].Rationale = "Check <value> & </pre> safely"
	results := []upstreamResult{result, {ID: "languageservices/working"}, {ID: "runner/new", Difference: true}}
	sources := []conformance.Source{
		{Name: "languageservices", Repository: "actions/languageservices", Revision: "pinned"},
		{Name: "runner", Repository: "actions/runner", Revision: "pinned"},
	}
	writeUpstreamSummary(t, filename, sources, results, map[string][]string{result.ID: result.Actual}, []upstreamAssessmentGroup{group})
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Reference observations", "## Reviewed assessments",
		"| [languageservices](https://github.com/actions/languageservices/tree/pinned) | 2 | 1 | 1 |",
		"| [runner](https://github.com/actions/runner/tree/pinned) | 1 | 0 | 1 |",
		"Current: 1. Stale: 0. Missing: 1.", "| extra-lint | 1 | 1 |",
		"category columns are not additive fixture totals",
		"<summary>Assessment inventory (1 groups)</summary>",
		"Check &lt;value&gt; &amp; &lt;/pre&gt; safely",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("summary missing %q:\n%s", want, data)
		}
	}
}

func TestUpstreamConformanceReaderSkips(t *testing.T) {
	root := fstest.MapFS{
		"workflow-parser/testdata/reader/insert.yml": &fstest.MapFile{Data: []byte(`skip:
  - TypeScript
---
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    stepz: []
---
{"errors": []}
`)},
	}
	cases := loadUpstreamWorkflows(t, root)
	if len(cases) != 1 || cases[0].Reject || !cmp.Equal(cases[0].ReferenceSkips, []string{"TypeScript"}) {
		t.Fatalf("reference metadata lost or changed the shared expectation: %+v", cases)
	}
	if actual := checkUpstreamCase(t, cases[0]); len(actual) == 0 {
		t.Fatal("reference skip prevented actionlint validation of the fixture")
	}
}
