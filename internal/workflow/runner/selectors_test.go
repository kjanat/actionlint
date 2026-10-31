package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestPreparedMalformedSelectorsRemainUnknown(t *testing.T) {
	for _, selection := range []string{
		`${{ null }}`,
		`${{ fromJSON('{"labels":["windows-latest",{}]}') }}`,
		`${{ fromJSON('{"labels":[["windows-latest"]]}') }}`,
		`${{ fromJSON('{"labels":"windows-latest","group":["prod"]}') }}`,
		`{labels: '${{ null }}'}`,
	} {
		t.Run(selection, func(t *testing.T) {
			w := runnerWorkflow(t, selection)
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			got := k.Job(w.Views["test"])
			if got.Platform != Any || got.Shell != "" || len(got.Findings) != 0 {
				t.Fatalf("invalid shape acquired meaning: %#v", got)
			}
		})
	}
}

func TestPartialSelectorsPreserveConstraints(t *testing.T) {
	for _, selection := range []string{
		`{group: prod, labels: ['${{ matrix.os }}', "${{ 'x64' }}"]}`,
		`${{ matrix.runner }}`,
	} {
		w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix:\n        os: [windows-latest]\n        runner: [{group: prod, labels: [windows-latest, x64]}]\n        unknown: '${{ fromJSON(vars.AXIS) }}'\n    runs-on: "+selection+"\n    steps: [{run: echo ok}]\n")
		selectors := Selectors(w.Views["test"])
		if len(selectors) != 2 {
			t.Fatalf("%#v", selectors)
		}
		s := selectors[1]
		if !s.Incomplete || s.Group == nil || s.Group.Value != runnerapi.Literal("prod") || len(s.Labels) != 2 || s.Labels[0].Value != runnerapi.Literal("windows-latest") || s.Labels[1].Value != runnerapi.Literal("x64") {
			t.Fatalf("lost constraints: %#v", s)
		}
		k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
		if err != nil {
			t.Fatal(err)
		}
		if got := k.Job(w.Views["test"]); got.Shell != "" || len(got.Findings) != 0 {
			t.Fatalf("partial alternatives claimed certainty: %#v", got)
		}
	}
}

func TestDynamicMatrixSelector(t *testing.T) {
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy: {matrix: '${{ fromJSON(vars.MATRIX) }}'}\n    runs-on: ${{ matrix.os }}\n    steps: [{run: echo ok}]\n")
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Job(w.Views["test"]); len(got.Findings) != 0 || got.Shell != "" {
		t.Fatalf("dynamic matrix inferred certainty: %#v", got)
	}
}

func TestPartialSelectorsKeepIndependentLabels(t *testing.T) {
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix:\n        os: [windows]\n        image: [ubuntu-lates]\n        include: '${{ fromJSON(vars.INCLUDE) }}'\n    runs-on: [linux, '${{ matrix.os }}', '${{ matrix.image }}']\n    steps: [{run: echo ok}]\n")
	selectors := Selectors(w.Views["test"])
	if len(selectors) != 3 {
		t.Fatalf("want separate fallback and two axis alternatives: %#v", selectors)
	}
	for i, selector := range selectors {
		if !selector.Incomplete || len(selector.Labels) != 3 || selector.Labels[0].Value != runnerapi.Literal("linux") {
			t.Fatalf("selector %d lost fixed labels or uncertainty: %#v", i, selector)
		}
	}
	if selectors[1].Labels[1].Value != runnerapi.Literal("windows") || selectors[1].Labels[2].Value != runnerapi.Expression("${{ matrix.image }}") || selectors[2].Labels[1].Value != runnerapi.Expression("${{ matrix.os }}") || selectors[2].Labels[2].Value != runnerapi.Literal("ubuntu-lates") {
		t.Fatalf("axis alternatives lost constraints or invented correlation: %#v", selectors)
	}
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Job(w.Views["test"]); got.Platform != Any || got.Shell != "" || len(got.Findings) != 2 {
		t.Fatalf("partial selectors lost independent diagnostics or claimed certainty: %#v", got)
	}
}

func TestFixedSelectorDoesNotDependOnDynamicMatrix(t *testing.T) {
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix:\n        item: '${{ fromJSON(vars.ITEMS) }}'\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n")
	selectors := Selectors(w.Views["test"])
	if len(selectors) != 1 || selectors[0].Incomplete || len(selectors[0].Labels) != 1 || selectors[0].Labels[0].Value != runnerapi.Literal("ubuntu-latest") {
		t.Fatalf("fixed selector acquired unrelated matrix uncertainty: %#v", selectors)
	}
	k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Job(w.Views["test"]); got.Platform != MacOrLinux || got.Shell != "bash" || len(got.Findings) != 0 {
		t.Fatalf("fixed runner lost shell facts: %#v", got)
	}
}

func TestPreparedNativeRunnerFindings(t *testing.T) {
	for _, tc := range []struct {
		selection string
		count     int
	}{
		{"ubuntu-latest", 0},
		{"[windows-latest, linux]", 1},
		{"unknown-runner", 1},
		{`${{ fromJSON('{"group":"prod","labels":["linux"]}') }}`, 0},
	} {
		w := runnerWorkflow(t, tc.selection)
		k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
		if err != nil {
			t.Fatal(err)
		}
		got := k.Job(w.Views["test"]).Findings
		if len(got) != tc.count {
			t.Fatalf("%s: %#v", tc.selection, got)
		}
	}
}

func TestMatrixIncludeUnresolvedLabelsKeepFixedConstraints(t *testing.T) {
	for _, item := range []string{`"${{ vars.RUNNER }}"`, `{name: windows-latest}`, `[windows-latest]`} {
		t.Run(item, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [a], include: [{os: a, runner: "+item+"}]}\n    runs-on: [self-hosted, '${{ matrix.runner }}']\n    steps: [{run: echo ok}]\n")
			selectors := Selectors(w.Views["test"])
			if len(selectors) != 1 || !selectors[0].Incomplete || len(selectors[0].Labels) != 2 || selectors[0].Labels[0].Value != runnerapi.Literal("self-hosted") || selectors[0].Labels[1].Value != runnerapi.Expression("${{ matrix.runner }}") {
				t.Fatalf("unresolved include lost constraints: %#v", selectors)
			}
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			if got := k.Job(w.Views["test"]); got.Platform != Any || got.Shell != "" || len(got.Findings) != 0 {
				t.Fatalf("unresolved include acquired runner facts: %#v", got)
			}
		})
	}
}

func TestDynamicMatrixFiltersKeepAlternativesIncomplete(t *testing.T) {
	for _, tc := range []struct{ name, matrix string }{
		{"exclude", `{os: [ubuntu-latest, windows-latest], exclude: [{os: "${{ vars.OS }}"}]}`},
		{"nested exclude", `{os: [ubuntu-latest, windows-latest], config: [{arch: x64}], exclude: [{os: ubuntu-latest, config: {arch: "${{ vars.ARCH }}"}}]}`},
		{"include", `{os: [ubuntu-latest], include: [{os: "${{ vars.OS }}", extra: x64}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: "+tc.matrix+"\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			selectors := Selectors(w.Views["test"])
			known := map[runnerapi.Term]bool{}
			for _, selector := range selectors {
				if !selector.Incomplete {
					t.Fatalf("dynamic filter supplied complete selector: %#v", selectors)
				}
				for _, label := range selector.Labels {
					known[label.Value] = true
				}
			}
			if !known[runnerapi.Literal("ubuntu-latest")] || tc.name != "include" && !known[runnerapi.Literal("windows-latest")] {
				t.Fatalf("dynamic filters dropped possible runner alternatives: %#v", selectors)
			}
			k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
			if err != nil {
				t.Fatal(err)
			}
			if got := k.Job(w.Views["test"]); got.Platform != Any || got.Shell != "" || len(got.Findings) != 0 {
				t.Fatalf("dynamic filter claimed certainty: %#v", got)
			}
		})
	}
}

func TestIncompleteMatrixFallbackOmitsDefinitelyExcludedLabels(t *testing.T) {
	for _, tc := range []struct {
		name, exclude, include string
		wantFindings           int
	}{
		{"known excluded typo", `[{os: ubuntu-lates}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 0},
		{"conditional exclusion retains typo", `[{os: ubuntu-lates, arch: x64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 1},
		{"exclusion union removes typo", `[{os: ubuntu-lates, arch: x64}, {os: ubuntu-lates, arch: arm64}]`, `"${{ fromJSON(vars.INCLUDE) }}"`, 0},
		{"explicit include reintroduces typo", `[{os: ubuntu-lates}]`, `[{os: ubuntu-lates}, "${{ fromJSON(vars.INCLUDE) }}"]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-latest, ubuntu-lates], arch: [x64, arm64], exclude: "+tc.exclude+", include: "+tc.include+"}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
			// This contract covers execution selectors. Declaration lint separately
			// retains excluded spelling errors in prepared job findings.
			answers, err := (builtinProvider{}).ResolveRunners(t.Context(), runnerapi.Request{Target: "github", Scope: "scope", Selectors: Selectors(w.Views["test"])})
			if err != nil {
				t.Fatal(err)
			}
			record, err := joinAnswers(w.Views["test"], answers)
			if err != nil {
				t.Fatal(err)
			}
			if got := record.knowledge(); len(got.Findings) != tc.wantFindings || got.Platform != Any || got.Shell != "" {
				t.Fatalf("excluded fallback label diagnostics or certainty: %#v", got)
			}
		})
	}
}

func TestRunnerMatrixPropertyRequiresWholeReference(t *testing.T) {
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
	w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [build-pool], include: [{os: BUILD-POOL}]}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
	selectors := Selectors(w.Views["test"])
	if len(selectors) != 1 || len(selectors[0].Labels) != 1 || selectors[0].Labels[0].Value != runnerapi.Literal("build-pool") {
		t.Fatalf("include changed the original axis: %#v", selectors)
	}
	if findings := Check(w.Views["test"], []string{"build-pool"}); len(findings) != 0 {
		t.Fatalf("complete combinations gained phantom include label: %v", findings)
	}
}

func TestRejectedMatrixExclusionsCannotSupplyRunnerCertainty(t *testing.T) {
	for _, filter := range []string{`[{os: windows-latest, nokey: x}]`, `"${{ fromJSON('[{}]') }}"`} {
		w := runnerSource(t, "on: push\njobs:\n  test:\n    strategy:\n      matrix: {os: [ubuntu-latest, windows-latest], exclude: "+filter+"}\n    runs-on: '${{ matrix.os }}'\n    steps: [{run: echo ok}]\n")
		selectors := Selectors(w.Views["test"])
		for _, selector := range selectors {
			if !selector.Incomplete {
				t.Fatalf("rejected exclusion supplied complete selector: %#v", selectors)
			}
		}
		k, err := Prepare(t.Context(), w.Snapshot, nil, runnerapi.Options{}, "scope")
		if err != nil {
			t.Fatal(err)
		}
		if got := k.Job(w.Views["test"]); got.Platform != Any || got.Shell != "" {
			t.Fatalf("rejected exclusion supplied runner facts: %#v", got)
		}
	}
}

func TestIncompleteMatrixOmitsOnlyProvedMergedIncludeAxisValues(t *testing.T) {
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
			findings := Check(w.Views["test"], []string{"build-pool"})
			if (len(findings) != 0) != tc.wantUpper {
				t.Fatalf("phantom or missing include findings: %v", findings)
			}
		})
	}
}
