package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestActionInputMappingInsertions(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"dynamic", "${{ fromJSON(vars.EXTRA) }}", ""},
		{"object", `${{ fromJSON('{"token":"value"}') }}`, ""},
		{"literal", "{token: '${{ github.token }}'}", ""},
		{"nested", "{'${{ insert }}': {token: '${{ github.token }}'}}", ""},
		{"empty", "{}", ""},
		{"conditional", `${{ fromJSON(false && 'bad JSON' || '{}') }}`, ""},
		{"case mapping", `${{ case(true, fromJSON('{"token":"value"}'), fromJSON('bad JSON')) }}`, ""},
		{"case skipped mapping", `${{ case(false, fromJSON('bad JSON'), fromJSON('{}')) }}`, ""},
		{"array", "${{ fromJSON('[]') }}", "with must be object"},
		{"nested array", `${{ fromJSON('{"token":[]}') }}`, "with.token must be string"},
		{"undefined", "${{ missing }}", `undefined variable "missing"`},
		{"invalid literal", "[]", "mapping node is expected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: example/action@v1
        with:
          path: source
          ${{ insert }}: ` + tc.value + "\n"
			errs := lintCredentialsExpressionTest(t, source)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestInsertedActionInputMetadata(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"supplies required", "{token: value}", ""},
		{"known object supplies required", `${{ fromJSON('{"token":"value"}') }}`, ""},
		{"unknown can supply required", "${{ fromJSON(vars.EXTRA) }}", ""},
		{"known empty misses required", "{}", `missing input "token"`},
		{"unknown name", "{token: value, typo: value}", `input "typo" is not defined`},
		{"known expression name", `${{ fromJSON('{"token":"value","typo":"value"}') }}`, `input "typo" is not defined`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n        with:\n          ${{ insert }}: " + tc.value + "\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			exec := workflow.Jobs["test"].Steps[0].Exec.(*ExecAction)
			rule := NewRuleAction(nil)
			rule.checkAction(&ActionMetadata{Inputs: map[string]*ActionMetadataInput{"token": {Name: "token", Required: true}}}, exec, func(*ActionMetadata) string { return "example/action@v1" })
			got := rule.Errs()
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatal(got)
				}
			} else if len(got) != 1 || !strings.Contains(got[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, got)
			}
		})
	}
}

func TestMatrixMappingInsertions(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		count        int
		want         string
	}{
		{"inserted axis", `base: [one], '${{ insert }}': "${{ fromJSON('{\"extra\":[1,2]}') }}"`, 2, ""},
		{"literal axis", `base: [one], '${{ insert }}': {extra: [1,2]}`, 2, ""},
		{"nested literal axis", `base: [one], '${{ insert }}': {'${{ insert }}': {extra: [1,2]}}`, 2, ""},
		{"case-selected insertion", `base: [one], '${{ insert }}': "${{ case(true, fromJSON('{\"extra\":[1,2]}'), fromJSON('bad JSON')) }}"`, 2, ""},
		{"empty insertion", `base: [one], '${{ insert }}': {}`, 1, ""},
		{"empty expression insertion", `base: [one], '${{ insert }}': "${{ fromJSON('{}') }}"`, 1, ""},
		{"dynamic insertion", `base: [one], '${{ insert }}': "${{ fromJSON(vars.EXTRA) }}"`, -1, ""},
		{"inserted exclude", `base: [one], '${{ insert }}': "${{ fromJSON('{\"extra\":[1,2],\"exclude\":[{\"extra\":1}]}') }}"`, 1, ""},
		{"inserted include", `base: [one], '${{ insert }}': "${{ fromJSON('{\"include\":[{\"base\":\"two\"}]}') }}"`, 2, ""},
		{"expression splicing", `item: [before, "${{ fromJSON('[\"one\",\"two\"]') }}", after]`, 4, ""},
		{"literal nested array", `item: [before, [one,two], after]`, 3, ""},
		{"empty splicing", `item: [before, "${{ fromJSON('[]') }}", after]`, 2, ""},
		{"include splicing", `include: [{item: before}, "${{ fromJSON('[{\"item\":\"one\"},{\"item\":\"two\"}]') }}", {item: after}]`, 4, ""},
		{"one level splicing", `item: ["${{ fromJSON('[[1,2],[3,4]]') }}"]`, 2, ""},
		{"literal result stays data", `base: [one], '${{ insert }}': "${{ fromJSON('{\"extra\":[\"${{ missing }}\"]}') }}"`, 1, ""},
		{"invalid source", `base: [one], '${{ insert }}': "${{ fromJSON('[]') }}"`, 0, "matrix insertion must be object"},
		{"invalid axis", `base: [one], '${{ insert }}': "${{ fromJSON('{\"extra\":1}') }}"`, 0, "must be array"},
		{"undefined source", `base: [one], '${{ insert }}': "${{ missing }}"`, 0, `undefined variable "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: {` + tc.matrix + `}
    steps: [{run: echo ok}]
`
			errs := lintCredentialsExpressionTest(t, source)
			if tc.want != "" {
				if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
					t.Fatalf("want %q, got %v", tc.want, errs)
				}
				return
			}
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			combinations, known := knownRunnerMatrixCombinations(workflow.Jobs["test"].Strategy)
			if tc.count < 0 {
				if known {
					t.Fatal("unknown insertion became a closed matrix")
				}
				return
			}
			if !known || len(combinations) != tc.count {
				t.Fatalf("want %d combinations, got known=%v %v", tc.count, known, combinations)
			}
		})
	}
}

func TestCaseMappingSelection(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{`case(true, false, 'fallback')`, ""},
		{`case(true, false, fromJSON('bad JSON'))`, ""},
		{`case(false, fromJSON('bad JSON'), false)`, ""},
		{`case(true, fromJSON('{"X":1}'), fromJSON('{}'))`, ""},
		{`case('false', 'yes', 'no')`, "predicate must be boolean"},
		{`case(1, 'yes', 'no')`, "predicate must be boolean"},
		{`case(null, 'yes', 'no')`, "predicate must be boolean"},
		{`case(false, 'yes', fromJSON('bad JSON'))`, "broken JSON"},
		{`case(true, false, unknownFunction())`, "undefined function"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          VALUE: ${{ toJSON(%s) }}\n", tc.expr)
			errs := lintCredentialsExpressionTest(t, source)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestMappingInsertionPreservesDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"inserted runner labels", `on: push
jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        base: [one]
        ${{ insert }}: ${{ fromJSON('{"os":["not-a-runner"]}') }}
    steps: [{run: echo ok}]
`, `label "not-a-runner" is unknown`},
		{"inserted duplicate axis", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        ${{ insert }}: ${{ fromJSON('{"item":["one","one"]}') }}
    steps: [{run: echo ok}]
`, "duplicate value"},
		{"inserted script input", `on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v8
        with:
          ${{ insert }}:
            script: "${{ github.event.issue.title }}"
`, "potentially untrusted"},
		{"unknown axis types", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        base: [one]
        ${{ insert }}: ${{ fromJSON(vars.EXTRA) }}
    steps: [{run: "echo ${{ matrix.extra }}"}]
`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := lintCredentialsExpressionTest(t, tc.source)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestUnsupportedStructuralTemplatesRemainRejected(t *testing.T) {
	for _, source := range []string{
		"on: push\njobs: ${{ fromJSON('{}') }}\n",
		"on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: ${{ fromJSON('[{\"run\":\"echo ok\"}]') }}\n",
		"on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          ${{ each value in vars.VALUES }}: text\n",
	} {
		if errs := lintCredentialsExpressionTest(t, source); len(errs) == 0 {
			t.Fatalf("unsupported structural template accepted: %s", source)
		}
	}
}
