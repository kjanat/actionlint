package expressions

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestUnsoundTernary(t *testing.T) {
	for _, tc := range []struct {
		expression string
		count      int
	}{
		{"github.ref && '' || 'fallback'", 1},
		{"github.ref && false || true", 1},
		{"github.ref && 0 || 1", 1},
		{"github.ref && -0.0 || 1", 1},
		{"github.ref && null || 'fallback'", 1},
		{"github.ref && fromJSON('false') || 'fallback'", 1},
		{"github.ref && join('') || 'fallback'", 1},
		{"github.ref && case(github.sha, '', false) || 'fallback'", 1},
		{"github.ref && case(github.sha, null, github.ref, 0, '') || 'fallback'", 1},
		{"github.ref && case(false, 'unreachable', github.sha, '', false) || 'fallback'", 1},
		{"github.ref && case(github.sha, '', true, false, github.ref) || 'fallback'", 1},
		{"github.ref && case(github.sha, case(github.ref, null, 0), '') || 'fallback'", 1},
		{"github.ref && !case(github.sha, 'false', '0') || 'fallback'", 1},
		{"github.ref && case(github.sha, '', 'truthy') || 'fallback'", 0},
		{"github.ref && case(github.sha, '', github.ref) || 'fallback'", 0},
		{"github.ref && case(github.sha, 'truthy', true, false, '') || 'fallback'", 0},
		{"github.ref && case(true, 'truthy', false) || 'fallback'", 0},
		{"github.ref && case(false, '', 'truthy') || 'fallback'", 0},
		{"github.ref && case(github.sha, fromJSON('broken'), false) || 'fallback'", 0},
		{"github.ref && case(github.sha, false, true, '', fromJSON('broken')) || 'fallback'", 1},
		{"github.ref && CASE(github.sha, '', false) || 'fallback'", 1},
		{"github.ref && case(false, fromJSON('broken'), github.sha, null, '') || 'fallback'", 1},
		{"github.ref && case(case(github.sha, true, 1), '', 'unreachable') || 'fallback'", 1},
		{"github.ref && case(github.sha, case(github.ref, false, true), '') || 'fallback'", 0},
		{"github.ref && case(github.sha, '') || 'fallback'", 0},
		{"github.ref && case() || 'fallback'", 0},
		{"github.ref && github.sha && false || 'fallback'", 1},
		{"github.ref && github.sha && '' || 'fallback'", 1},
		{"github.ref && github.sha && null || 'fallback'", 1},
		{"github.ref && github.sha && 0 || 'fallback'", 1},
		{"(github.ref && github.sha) && false || 'fallback'", 1},
		{"github.ref && !(github.sha || true) || 'fallback'", 1},
		{"github.ref && (github.sha && false || false) || 'fallback'", 2},
		{"format('{0}', github.ref && '' || 'fallback')", 1},
		{"(github.ref && (github.sha && '' || 'ok')) || 'fallback'", 1},
		{"github.ref && 'false' || 'fallback'", 0},
		{"github.ref && '0' || 'fallback'", 0},
		{"github.ref && github.sha || 'fallback'", 0},
		{"github.ref && github.sha && true || 'fallback'", 0},
		{"github.ref && (github.sha || false) || 'fallback'", 0},
		{"github.ref && (github.sha || true) || 'fallback'", 0},
		{"github.ref && fromJSON('[]') || 'fallback'", 0},
		{"github.ref && (false || true)", 0},
		{"github.ref || ''", 0},
		{"github.ref && fromJSON('broken') || 'fallback'", 0},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			rule := newTestChecker()
			rule.checkExprsIn("${{ "+tc.expression+" }}", &workflowlang.Pos{Line: 3, Col: 5}, false, false, "env")
			count := 0
			for _, finding := range rule.Findings() {
				if finding.Rule == "unsound-ternary" {
					count++
					if finding.Start.Line != 3 || finding.Start.Column < 5 {
						t.Fatalf("bad location: %+v", finding)
					}
				}
			}
			if count != tc.count {
				t.Fatalf("got %d findings, want %d: %+v", count, tc.count, rule.Findings())
			}
		})
	}
}
