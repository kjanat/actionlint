package matrix

import (
	"strings"
	"testing"

	matrixmodel "actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestInvalidExclusionsRejectCompleteProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, filter, diagnostic string }{
		{"missing axis", `[{os: windows-latest, nokey: x}]`, `"nokey" in "exclude" section does not exist in matrix. available matrix configurations are "os"`},
		{"empty filter", `"${{ fromJSON('[{}]') }}"`, `matrix filter in "exclude" must not be empty`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: '${{ matrix.os }}'\n    strategy:\n      matrix:\n        os: [ubuntu-latest, windows-latest]\n        exclude: " + tc.filter + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := matrixmodel.Resolve(workflow.Jobs["test"].Strategy)
			if combinations, complete := resolution.Combinations(16); complete || len(combinations) != 0 {
				t.Fatalf("invalid exclusion supplied execution combinations: %v", combinations)
			}
			if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 2 {
				t.Fatalf("invalid exclusion narrowed execution alternatives: %#v", reach)
			}
			findings := Check(resolution, false)
			if len(findings) != 1 || findings[0].Message != tc.diagnostic {
				t.Fatalf("invalid exclusion diagnostic=%v, want %s", findings, tc.diagnostic)
			}
			filters := workflow.Jobs["test"].Strategy.Matrix.Exclude
			if tc.name == "empty filter" {
				if filters.Expression == nil || filters.Expression.Pos == nil || findings[0].Start.Line != filters.Expression.Pos.Line || findings[0].Start.Column != filters.Expression.Pos.Col {
					t.Fatalf("empty expression filter fabricated source position: %v", findings)
				}
			} else {
				key := filters.Combinations[0].Assigns["nokey"].Key
				if key == nil || key.Pos == nil || findings[0].Start.Line != key.Pos.Line || findings[0].Start.Column != key.Pos.Col {
					t.Fatalf("missing axis diagnostic lost key origin: %v", findings)
				}
			}
		})
	}
}

func TestOpenMatrixAxisDomainDoesNotInventExclusionIssues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, matrix string }{
		{"unknown insertion only", `{'${{ insert }}': "${{ fromJSON(vars.AXES) }}", exclude: [{os: ubuntu}]}`},
		{"unknown insertion with known axis", `{os: [a], '${{ insert }}': "${{ fromJSON(vars.AXES) }}", exclude: [{arch: x}]}`},
		{"unknown row name", `{"${{ vars.KEY }}": [a], os: [b], exclude: [{k: a}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := matrixmodel.Resolve(workflow.Jobs["test"].Strategy)
			if findings := Check(resolution, false); len(findings) != 0 {
				t.Fatalf("open axis domain invented a missing-axis error: %v", findings)
			}
			if _, complete := resolution.Combinations(16); complete {
				t.Fatal("open axis domain acquired complete combinations")
			}
			axes, _, found := strings.Cut(tc.matrix, "exclude:")
			if !found {
				t.Fatal("test matrix has no exclusion declaration to replace")
			}
			emptySource := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + axes + `exclude: "${{ fromJSON('[{}]') }}"}` + "\n    steps: [{run: echo ok}]\n"
			emptyWorkflow, emptyErrors := workflowparser.Parse([]byte(emptySource))
			if len(emptyErrors) != 0 {
				t.Fatal(emptyErrors)
			}
			resolution = matrixmodel.Resolve(emptyWorkflow.Jobs["test"].Strategy)
			findings := Check(resolution, false)
			if len(findings) != 1 || findings[0].Message != `matrix filter in "exclude" must not be empty` {
				t.Fatalf("open axis domain hid an empty exclusion: %v", findings)
			}
		})
	}
}
