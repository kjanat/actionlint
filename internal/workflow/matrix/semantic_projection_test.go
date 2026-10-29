package matrix

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestIncludesMatchOriginalAxes(t *testing.T) {
	for _, tc := range []struct{ name, matrix, want string }{
		{"overwrite extras", `{"os":["linux","windows"],"include":[{"image":"ubuntu"},{"os":"windows","image":"windows"}]}`, `[{"os":"linux","image":"ubuntu"},{"os":"windows","image":"windows"}]`},
		{"partial object preserves axis", `{"config":[{"name":"build","version":24}],"include":[{"image":"ubuntu"},{"config":{"name":"build"},"image":"windows"}]}`, `[{"config":{"name":"build","version":24},"image":"windows"}]`},
		{"partial array preserves axis", `{"config":[["build",24]],"include":[{"image":"ubuntu"},{"config":["build"],"image":"windows"}]}`, `[{"config":["build",24],"image":"windows"}]`},
		{"standalone rows stay isolated", `{"os":["linux"],"include":[{"os":"windows","image":"first"},{"os":"windows","extra":"second"}]}`, `[{"os":"linux"},{"os":"windows","image":"first"},{"os":"windows","extra":"second"}]`},
		{"include only rows", `{"include":[{"os":"linux"},{"os":"windows"},{"extra":"third"}]}`, `[{"os":"linux"},{"os":"windows"},{"extra":"third"}]`},
		{"excluded originals stay excluded", `{"os":["linux"],"exclude":[{"os":"linux"}],"include":[{"os":"linux","image":"first"},{"os":"linux","extra":"second"}]}`, `[{"os":"linux","image":"first"},{"os":"linux","extra":"second"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			want := value.Template("${{ fromJSON('"+tc.want+"') }}", value.Origin{})
			for range 2 {
				combinations, complete := resolution.Combinations(16)
				if !complete || len(combinations) != want.Len() {
					t.Fatalf("got %d combinations, complete=%t; want %d", len(combinations), complete, want.Len())
				}
				for i, combination := range combinations {
					var props []value.Prop
					for name, item := range combination.Values() {
						props = append(props, value.Prop{Name: name, Value: item})
					}
					expected, _ := want.Index(i)
					if !value.Map(value.Origin{}, props...).Equal(expected) {
						t.Fatalf("combination %d differs from %s", i, tc.want)
					}
				}
			}
		})
	}
}

func TestNestedArrayExpressionsPreserveInterpretationBoundary(t *testing.T) {
	for _, tc := range []struct{ name, matrix, expected string }{
		{"nested object expression", `item: [[{name: "${{ 'known' }}"}]]`, `[{"name":"known"}]`},
		{"nested array insertion", `item: [[["${{ fromJSON('[1,2]') }}"]]]`, `[[1,2]]`},
		{"returned nested syntax stays data", `item: [["${{ fromJSON('[{\"name\":\"${{ vars.NAME }}\"}]') }}"]]`, `[{"name":"${{ vars.NAME }}"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			reach := Resolve(workflow.Jobs["test"].Strategy).Reachable("item")
			want := value.Template("${{ fromJSON('"+tc.expected+"') }}", value.Origin{})
			if !reach.Complete || len(reach.Values) != 1 || !reach.Values[0].Equal(want) {
				t.Fatalf("nested value differs from %s; complete=%t, values=%v", tc.expected, reach.Complete, reach.Values)
			}
		})
	}
}

func TestIncludeCompletenessIndependentOfExcludes(t *testing.T) {
	for _, tc := range []struct {
		name, filters string
		complete      bool
	}{
		{"no filters", "", true},
		{"unknown excludes", `, exclude: "${{ fromJSON(vars.EXCLUDE) }}"`, true},
		{"known includes with unknown excludes", `, include: [{item: 7}], exclude: "${{ fromJSON(vars.EXCLUDE) }}"`, true},
		{"dynamic include value retained", `, include: [{item: "${{ github.head_ref }}"}]`, true},
		{"unknown include array", `, include: "${{ fromJSON(vars.INCLUDE) }}"`, false},
		{"unknown include insertion", `, include: ["${{ fromJSON(vars.INCLUDE) }}"]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {item: [42]" + tc.filters + "}\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			if got := Resolve(workflow.Jobs["test"].Strategy).IncludesComplete(); got != tc.complete {
				t.Fatalf("includes complete=%t, want %t", got, tc.complete)
			}
		})
	}
}

func TestDynamicFilterComparisonsCannotEnumerateDefiniteCombinations(t *testing.T) {
	for _, filters := range []string{
		`exclude: [{os: "${{ vars.OS }}"}]`,
		`exclude: [{os: ubuntu, config: {arch: "${{ vars.ARCH }}"}}]`,
		`include: [{os: "${{ vars.OS }}", extra: x64}]`,
	} {
		workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {os: [ubuntu, windows], config: [{arch: x64}], " + filters + "}\n    steps: [{run: echo ok}]\n"))
		if len(errors) != 0 {
			t.Fatal(errors)
		}
		if combinations, complete := Resolve(workflow.Jobs["test"].Strategy).Combinations(16); complete || len(combinations) != 0 {
			t.Fatalf("dynamic comparison enumerated definite combinations: complete=%t, combinations=%v", complete, combinations)
		}
	}
	semantic := value.Map(value.Origin{}, value.Prop{Name: "os", Value: value.Seq(value.Origin{}, value.DynamicValue("${{ vars.OS }}", value.KindUnknown, value.Origin{}))})
	if combinations, complete := resolveSemanticMatrix(semantic).Combinations(16); complete || len(combinations) != 0 {
		t.Fatal("dynamic actual value enumerated definite combinations")
	}
}

func TestDuplicateInsertedFiltersFailClosed(t *testing.T) {
	for _, name := range []string{"include", "exclude"} {
		workflow, errors := workflowparser.ParseDocument([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [ubuntu, windows]\n        " + name + ": [{os: ubuntu}]\n        '${{ insert }}': {" + name + ": [{os: windows}]}\n    steps: [{run: echo ok}]\n"))
		if len(errors) != 1 {
			t.Fatalf("want parser duplicate-key diagnostic for %s, got %v", name, errors)
		}
		resolution := Resolve(workflow.Jobs["test"].Strategy)
		if resolution.State() != value.Invalid {
			t.Fatalf("duplicate %s became valid after projection: %v", name, resolution.State())
		}
		if combinations, complete := resolution.Combinations(16); complete || len(combinations) != 0 {
			t.Fatalf("duplicate %s enumerated definite combinations: %v", name, combinations)
		}
		if name == "include" && len(resolution.include) != 2 || name == "exclude" && len(resolution.exclude) != 2 {
			t.Fatalf("duplicate %s discarded declaration evidence", name)
		}
		workflow, errors = workflowparser.ParseDocument([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [ubuntu]\n        " + name + ": \"${{ fromJSON('[]') }}\"\n        '${{ insert }}': {" + name + ": \"${{ fromJSON('[]') }}\"}\n    steps: [{run: echo ok}]\n"))
		if len(errors) != 1 || Resolve(workflow.Jobs["test"].Strategy).State() != value.Invalid {
			t.Fatalf("empty duplicate %s lost declaration presence: %v", name, errors)
		}
	}
}

func TestPostExcludeFallbackRetainsUncertainCandidates(t *testing.T) {
	ubuntu := value.YAMLScalar("ubuntu-latest", "!!str", value.Origin{})
	typo := value.YAMLScalar("ubuntu-lates", "!!str", value.Origin{})
	var largeAxis []value.Value
	for i := range 257 {
		largeAxis = append(largeAxis, value.FromJSON(i, value.Origin{}))
	}
	resolution := Resolution{
		state: value.Literal,
		axes: []Axis{
			{Name: "os", Values: []value.Value{ubuntu, typo}, Complete: true},
			{Name: "arch", Values: largeAxis, Complete: true},
		},
		exclude: []Combination{(Combination{}).With("os", typo).With("arch", largeAxis[0])},
	}
	if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 2 {
		t.Fatalf("bounded fallback dropped possible candidates: %#v", reach)
	}
	resolution.axes[1] = Axis{Name: "arch", Values: []value.Value{value.DynamicValue("${{ vars.ARCH }}", value.KindUnknown, value.Origin{})}}
	if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 2 {
		t.Fatalf("dynamic fallback dropped possible candidates: %#v", reach)
	}
	resolution.exclude = []Combination{(Combination{}).With("os", typo)}
	if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 1 || !reach.Values[0].Equal(ubuntu) {
		t.Fatalf("unconditional exclusion lost its proof: %#v", reach)
	}
}

func TestPostExcludeProjectionDoesNotNarrowInvalidOrDynamicMatrix(t *testing.T) {
	for _, state := range []value.State{value.Invalid, value.Dynamic} {
		os := value.YAMLScalar("ubuntu-lates", "!!str", value.Origin{})
		resolution := Resolution{
			state:   state,
			axes:    []Axis{{Name: "os", Values: []value.Value{os}, Complete: true}},
			exclude: []Combination{(Combination{}).With("os", os)},
		}
		if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 1 || !reach.Values[0].Equal(os) {
			t.Fatalf("state %v narrowed uncertain alternatives: %#v", state, reach)
		}
	}
}

func TestSemanticMatrixDuplicateFilterKeysFailClosed(t *testing.T) {
	for _, name := range []string{"include", "exclude"} {
		upper := "Include"
		if name == "exclude" {
			upper = "Exclude"
		}
		first := value.Seq(value.Origin{}, value.Map(value.Origin{}, value.Prop{Name: "os", Value: value.YAMLScalar("ubuntu", "!!str", value.Origin{})}))
		second := value.Seq(value.Origin{}, value.Map(value.Origin{}, value.Prop{Name: "os", Value: value.YAMLScalar("windows", "!!str", value.Origin{})}))
		semantic := value.Map(value.Origin{},
			value.Prop{Name: "os", Value: value.Seq(value.Origin{}, value.YAMLScalar("ubuntu", "!!str", value.Origin{}), value.YAMLScalar("windows", "!!str", value.Origin{}))},
			value.Prop{Name: name, Value: first},
			value.Prop{Name: upper, Value: second},
		)
		resolution := resolveSemanticMatrix(semantic)
		if resolution.State() != value.Invalid || resolution.IncludesComplete() || resolution.filterKnown {
			t.Fatalf("duplicate semantic %s acquired completeness: %#v", name, resolution)
		}
		if combinations, complete := resolution.Combinations(16); complete || len(combinations) != 0 {
			t.Fatalf("duplicate semantic %s enumerated combinations", name)
		}
		if name == "include" && len(resolution.include) != 2 || name == "exclude" && len(resolution.exclude) != 2 {
			t.Fatalf("duplicate semantic %s lost declaration evidence", name)
		}
	}
}

func TestIncludeAddedKeyOriginsSurviveImmutableCopies(t *testing.T) {
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [ubuntu]\n        include:\n          - os: ubuntu\n            extra: first\n          - extra: second\n          - os: windows\n            extra: separate\n    steps: [{run: echo ok}]\n"
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	resolution := Resolve(workflow.Jobs["test"].Strategy)
	for range 2 {
		combinations, complete := resolution.Combinations(16)
		if !complete || len(combinations) != 2 {
			t.Fatalf("expected merged original and standalone include: %v", combinations)
		}
		for i, line := range []int{11, 13} {
			original := combinations[i]
			want := diagnostic.Position{Line: line, Column: 13}
			if got := original.keyOrigin("extra").Span.Start; got != want {
				t.Fatalf("include key origin=%v, want %v", got, want)
			}
			newOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 99, Column: 2}}}
			derived := original.With("new", value.YAMLScalar("new", "!!str", newOrigin)).With("extra", value.YAMLScalar("replacement", "!!str", newOrigin))
			if original.keyOrigin("extra").Span.Start != want || derived.keyOrigin("extra").Span.Start != want || derived.keyOrigin("new") != newOrigin || derived.origin != original.origin {
				t.Fatal("include key metadata lost through immutable copy")
			}
			if _, exists := original.keyOrigins["new"]; exists {
				t.Fatal("derived combination mutated original include metadata")
			}
		}
	}
}

func TestInvalidExclusionsRejectCompleteProjection(t *testing.T) {
	for _, tc := range []struct{ name, filter, diagnostic string }{
		{"missing axis", `[{os: windows-latest, nokey: x}]`, `"nokey" in "exclude" section does not exist in matrix. available matrix configurations are "os"`},
		{"empty filter", `"${{ fromJSON('[{}]') }}"`, `matrix filter in "exclude" must not be empty`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: '${{ matrix.os }}'\n    strategy:\n      matrix:\n        os: [ubuntu-latest, windows-latest]\n        exclude: " + tc.filter + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			if combinations, complete := resolution.Combinations(16); complete || len(combinations) != 0 {
				t.Fatalf("invalid exclusion supplied execution combinations: %v", combinations)
			}
			if reach := resolution.ReachableAfterExcludes("os"); reach.Complete || len(reach.Values) != 2 {
				t.Fatalf("invalid exclusion narrowed execution alternatives: %#v", reach)
			}
			findings := resolution.Check(false)
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

func TestExclusionMatcherRequiresKnownPresentKeys(t *testing.T) {
	windows := value.YAMLScalar("windows-latest", "!!str", value.Origin{})
	actual := (Combination{}).With("os", windows)
	for _, filter := range []Combination{
		{},
		(Combination{}).With("OS", windows).With("nokey", windows),
		(Combination{}).With("OS", value.DynamicValue("${{ vars.OS }}", value.KindUnknown, value.Origin{})),
	} {
		if exclusionMatches(actual.exclusionValues, filter) {
			t.Fatalf("invalid or unknown filter supplied a definite match: %#v", filter)
		}
	}
	if !exclusionMatches(actual.exclusionValues, (Combination{}).With("OS", windows)) {
		t.Fatal("case-variant exclusion did not match its axis")
	}
	semantic := value.Template(`${{ fromJSON('{"os":["ubuntu-latest","windows-latest"],"exclude":[{"OS":"windows-latest"}]}') }}`, value.Origin{})
	resolution := resolveSemanticMatrix(semantic)
	combinations, complete := resolution.Combinations(16)
	if !complete || len(combinations) != 1 {
		t.Fatalf("valid case-variant exclusion failed: complete=%t, combinations=%v", complete, combinations)
	}
	if os, _ := combinations[0].Get("os"); !os.Equal(value.YAMLScalar("ubuntu-latest", "!!str", value.Origin{})) {
		t.Fatal("case-variant exclusion retained the wrong original")
	}
}

func TestOpenMatrixAxisDomainDoesNotInventExclusionIssues(t *testing.T) {
	for _, tc := range []struct{ name, matrix string }{
		{"unknown insertion only", `{'${{ insert }}': "${{ fromJSON(vars.AXES) }}", exclude: [{os: ubuntu}]}`},
		{"unknown insertion with known axis", `{os: [a], '${{ insert }}': "${{ fromJSON(vars.AXES) }}", exclude: [{arch: x}]}`},
		{"unknown row name", `{"${{ vars.KEY }}": [a], os: [b], exclude: [{k: a}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			if findings := resolution.Check(false); len(findings) != 0 {
				t.Fatalf("open axis domain invented a missing-axis error: %v", findings)
			}
			if _, complete := resolution.Combinations(16); complete {
				t.Fatal("open axis domain acquired complete combinations")
			}
			resolution.exclude = []Combination{{values: map[string]value.Value{}}}
			findings := resolution.Check(false)
			if len(findings) != 1 || findings[0].Message != `matrix filter in "exclude" must not be empty` {
				t.Fatalf("open axis domain hid an empty exclusion: %v", findings)
			}
		})
	}
}

func TestIncludeApplicabilityUsesSubsetAndSurvivingOriginals(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		want         matchProof
	}{
		{"case-insensitive axis", `{os: [build-pool], include: [{os: BUILD-POOL}, "${{ fromJSON(vars.INCLUDE) }}"]}`, matchAlways},
		{"partial object axis", `{os: [linux], config: [{name: build, version: 24}], include: [{config: {name: BUILD}}]}`, matchAlways},
		{"partial array axis", `{os: [linux], config: [[build, 24]], include: [{config: [BUILD]}]}`, matchAlways},
		{"excluded original", `{os: [build-pool], exclude: [{os: build-pool}], include: [{os: BUILD-POOL}]}`, matchNever},
		{"unknown excludes", `{os: [build-pool], exclude: "${{ fromJSON(vars.EXCLUDE) }}", include: [{os: BUILD-POOL}]}`, matchUncertain},
		{"dynamic exclusion can match", `{os: [build-pool], exclude: [{os: "${{ vars.OS }}"}], include: [{os: BUILD-POOL}]}`, matchUncertain},
		{"known mismatch disproves dynamic exclusion", `{os: [build-pool], arch: [x64], exclude: [{os: other, arch: "${{ vars.ARCH }}"}], include: [{os: BUILD-POOL}]}`, matchAlways},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			inclusion := resolution.include[0]
			if got := resolution.includeApplicability(inclusion); got != tc.want {
				t.Fatalf("include applicability=%v, want %v", got, tc.want)
			}
			if got := resolution.includeIsStandalone(inclusion); got != (tc.want != matchAlways) {
				t.Fatalf("standalone decision disagrees with applicability: %t", got)
			}
			if tc.name == "partial object axis" || tc.name == "partial array axis" {
				if resolution.Reachable("os").MayBeMissing {
					t.Fatal("merged partial include invented a missing axis")
				}
			}
		})
	}
}

func TestIncludeMergeProofBeyondCombinationBudget(t *testing.T) {
	os := value.YAMLScalar("build-pool", "!!str", value.Origin{})
	upper := value.YAMLScalar("BUILD-POOL", "!!str", value.Origin{})
	inclusion := (Combination{}).With("os", upper)
	var largeAxis []value.Value
	for i := range 257 {
		largeAxis = append(largeAxis, value.FromJSON(i, value.Origin{}))
	}
	resolution := Resolution{
		state: value.Literal, excludeKnown: true,
		axes: []Axis{
			{Name: "os", Values: []value.Value{os}, Complete: true},
			{Name: "extra", Values: largeAxis, Complete: true},
		},
		include: []Combination{inclusion},
	}
	if got := resolution.includeApplicability(inclusion); got != matchAlways {
		t.Fatalf("bounded enumeration lost known surviving witness: %v", got)
	}
	if reach := resolution.ReachableAfterExcludes("os"); len(reach.Values) != 1 || !reach.Values[0].Equal(os) {
		t.Fatalf("proved merged include manufactured an axis alternative: %#v", reach)
	}
	resolution.axes[1] = Axis{Name: "extra", Values: []value.Value{value.DynamicValue("${{ fromJSON(vars.EXTRA) }}", value.KindUnknown, value.Origin{})}}
	if got := resolution.includeApplicability(inclusion); got != matchUncertain {
		t.Fatalf("unknown axis invented a surviving witness: %v", got)
	}
	if reach := resolution.ReachableAfterExcludes("os"); len(reach.Values) != 2 {
		t.Fatalf("uncertain include reintroduction was discarded: %#v", reach)
	}
}
