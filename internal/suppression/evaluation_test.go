package suppression

import (
	"testing"
)

func TestEvaluationPolicyAndDeclarationRanges(t *testing.T) {
	const source = "run: | # actionlint:ignore expression,shellcheck -- reviewed\n  echo first\n  echo second\nnext: value\n"
	directives, invalid := Parse([]byte(source), []string{"expression", "shellcheck"})
	if len(invalid) != 0 || len(directives) != 1 {
		t.Fatalf("invalid block directive: %+v, %+v", directives, invalid)
	}
	for _, tc := range []struct {
		name     string
		decision Decision
	}{
		{"allowed", Decision{Suppress: true}},
		{"report exception", Decision{Suppress: true, Report: true}},
		{"retain violation", Decision{}},
		{"report both", Decision{Report: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, reports := Evaluate(directives, func(rule string) Decision {
				if rule == "expression" {
					return tc.decision
				}
				return Decision{Suppress: true}
			})
			for _, extent := range [][2]int{{1, 1}, {2, 2}, {3, 3}, {2, 4}} {
				if got := result.Suppresses("expression", extent[0], extent[1]); got != tc.decision.Suppress {
					t.Fatalf("origin %v: suppressed=%v, want %v", extent, got, tc.decision.Suppress)
				}
			}
			if result.Suppresses("expression", 4, 4) || result.Suppresses("other", 2, 2) {
				t.Fatal("exception escaped its declaration or rule")
			}
			if !result.Suppresses("shellcheck", 2, 2) {
				t.Fatal("restriction on one rule changed another rule")
			}
			wantReports := 0
			if tc.decision.Report {
				wantReports = 1
			}
			if len(reports) != wantReports {
				t.Fatalf("policy diagnostics: %+v", reports)
			}
			if wantReports != 0 && (reports[0].Rule != "disallow-suppressions" || reports[0].Start.Line != 1 || reports[0].Message != "inline suppression of expression is disallowed by policy.disallow-suppressions") {
				t.Fatalf("lost policy diagnostic identity: %+v", reports)
			}
		})
	}
}

func TestInvalidDirectiveDoesNotSuppress(t *testing.T) {
	for _, source := range []string{
		"run: echo ok # actionlint:ignore unknown -- reviewed",
		"run: echo ok # actionlint:ignore expression --",
		"run: echo ok # actionlint:ignore-next-line expression -- reviewed",
	} {
		directives, invalid := Parse([]byte(source), []string{"expression"})
		if len(directives) != 0 || len(invalid) != 1 || invalid[0].Rule != "inline-suppression" {
			t.Fatalf("invalid directive accepted: %q: %+v, %+v", source, directives, invalid)
		}
		if evaluation, _ := Evaluate(directives, nil); evaluation.Suppresses("expression", 1, 1) {
			t.Fatalf("invalid directive hid a finding: %q", source)
		}
	}
}
