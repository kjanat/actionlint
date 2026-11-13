package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestIncompleteMatrixFallbackOmitsDefinitelyExcludedLabels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, exclude, include string
		wantRejections         int
	}{
		{"known excluded typo", `[{os: ubuntu-lates}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 0},
		{"conditional exclusion retains typo", `[{os: ubuntu-lates, arch: x64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"exclusion union removes typo", `[{os: ubuntu-lates, arch: x64}, {os: ubuntu-lates, arch: arm64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 0},
		{"explicit include reintroduces typo", `[{os: ubuntu-lates}]`, `[{os: ubuntu-lates}, "${{ fromJSON(vars.INCLUDE) }}"]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-latest, ubuntu-lates], arch: [x64, arm64], exclude: "+tc.exclude+", include: "+tc.include+"}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			// This contract covers execution selectors. Declaration lint separately
			// retains excluded spelling errors in prepared job findings.
			answers, err := (builtinProvider{custom: KnownLabels(nil)}).ResolveRunners(t.Context(), runnerapi.Request{Target: "github", Scope: "scope", Selectors: Selectors(w.Views["test"])})
			if err != nil {
				t.Fatal(err)
			}
			record, err := joinAnswers(w.Views["test"], answers)
			if err != nil {
				t.Fatal(err)
			}
			if got := record.knowledge(); len(got.Rejections) != tc.wantRejections || got.Platform != Any || got.Shell != "" {
				t.Fatalf("excluded fallback label diagnostics or certainty: %#v", got)
			}
		})
	}
}

func TestRunnerMatrixPropertyRequiresWholeReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, want string }{
		{`${{ matrix.os }}`, "os"},
		{"  ${{ MATRIX.OS }}  ", "os"},
		{`${{ matrix['os'] }}`, "os"},
		{`${{ matrix.os }}-latest`, ""},
		{`prefix-${{ matrix.os }}`, ""},
		{`matrix.os`, ""},
		{`${{ matrix.os }`, ""},
		{`${{ matrix.os || 'ubuntu-latest' }}`, ""},
		{`${{ matrix.os.name }}`, ""},
		{`${{ matrix[vars.KEY] }}`, ""},
	} {
		if got := runnerMatrixProperty(tc.expression); got != tc.want {
			t.Errorf("property(%q)=%q, want %q", tc.expression, got, tc.want)
		}
	}
}

func TestRunnerCheckCompleteMatrixDoesNotAddPhantomIncludeAlternative(t *testing.T) {
	t.Parallel()
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [build-pool], include: [{os: BUILD-POOL}]}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
	selectors := Selectors(w.Views["test"])
	if len(selectors) != 1 || len(selectors[0].Labels) != 1 || selectors[0].Labels[0].Value != runnerapi.Literal("build-pool") {
		t.Fatalf("include changed the original axis: %#v", selectors)
	}
	if findings := LabelRejections(w.Views["test"], KnownLabels([]string{"build-pool"})); len(findings) != 0 {
		t.Fatalf("complete combinations gained phantom include label: %v", findings)
	}
}

func TestIncompleteMatrixOmitsOnlyProvedMergedIncludeAxisValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, matrix string
		wantUpper    bool
	}{
		{"unknown includes", `{os: [build-pool], include: [{os: BUILD-POOL}, "${{ fromJSON(vars.INCLUDE) }}"]}`, false},
		{"known surviving original", `{os: [build-pool], exclude: [{os: other}], include: [{os: BUILD-POOL}, "${{ fromJSON(vars.INCLUDE) }}"]}`, false},
		{"known exclusion reintroduces include", `{os: [build-pool], exclude: [{os: build-pool}], include: [{os: BUILD-POOL}, "${{ fromJSON(vars.INCLUDE) }}"]}`, true},
		{"unknown excludes may reintroduce include", `{os: [build-pool], exclude: "${{ fromJSON(vars.EXCLUDE) }}", include: [{os: BUILD-POOL}]}`, true},
		{"dynamic exclusion may reintroduce include", `{os: [build-pool], exclude: [{os: "${{ vars.OS }}"}], include: [{os: BUILD-POOL}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: "+tc.matrix+"\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			upper := false
			for _, selector := range Selectors(w.Views["test"]) {
				if !selector.Incomplete {
					t.Fatal("incomplete matrix acquired a complete selector")
				}
				for _, constraint := range selector.Labels {
					upper = upper || constraint.Value == runnerapi.Literal("BUILD-POOL")
				}
			}
			if upper != tc.wantUpper {
				t.Fatalf("uppercase alternative=%t, want %t", upper, tc.wantUpper)
			}
			findings := LabelRejections(w.Views["test"], KnownLabels([]string{"build-pool"}))
			if (len(findings) != 0) != tc.wantUpper {
				t.Fatalf("phantom or missing include findings: %v", findings)
			}
		})
	}
}
