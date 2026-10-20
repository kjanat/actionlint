package expressions

import (
	"maps"
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestSafeMatrixScriptReferences(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		paths        []string
	}{
		{"literal axis", `{target: [linux, windows]}`, []string{"matrix.target"}},
		{"dangerous alternate", `{target: [linux, 'bad"value']}`, nil},
		{"known include", `{target: [linux], include: [{target: windows, extra: safe}]}`, []string{"matrix.target", "matrix.extra"}},
		{"dangerous include", `{target: [linux], include: [{target: 'bad"value'}]}`, nil},
		{"unknown include", `{target: [linux], include: "${{ fromJSON(inputs.extra) }}"}`, nil},
		{"unknown include entry", `{target: [linux], include: ["${{ fromJSON(inputs.extra) }}"]}`, nil},
		{"unknown axis", `{target: [linux], other: "${{ fromJSON(inputs.extra) }}"}`, nil},
		{"unknown nested value", `{target: [{name: linux, extra: "${{ inputs.extra }}"}]}`, nil},
		{"nested object", `{target: [{name: linux, version: 1}, {name: windows, version: 2}]}`, []string{"matrix.target.name", "matrix.target.version", "matrix.target[*]"}},
		{"nested dangerous alternate", `{target: [{name: linux}, {name: 'bad"value'}]}`, nil},
		{"dotted key cannot certify another path", `{target: [{"item.name": linux, item: {name: 'bad"value'}}]}`, nil},
		{"dotted axis cannot certify another path", `{"target.name": [linux], target: [{name: 'bad"value'}]}`, nil},
		{"exclude does not manufacture proof", `{target: [linux, 'bad"value'], exclude: [{target: 'bad"value'}]}`, nil},
		{"literal row expression", `{target: "${{ fromJSON('[\"linux\",\"windows\"]') }}"}`, []string{"matrix.target"}},
		{"literal whole matrix", `"${{ fromJSON('{\"target\":[\"linux\",\"windows\"]}') }}"`, []string{"matrix.target"}},
		{"literal include expression", `{include: "${{ fromJSON('[{\"target\":\"linux\"},{\"target\":\"windows\"}]') }}"}`, []string{"matrix.target"}},
		{"literal nested fromJSON", `{target: [{item: "${{ fromJSON('{\"name\":\"linux\"}') }}"}]}`, []string{"matrix.target.item.name", "matrix.target.item[*]"}},
		{"nested array", `{target: [[[linux]]]}`, []string{"matrix.target[0][0]", "matrix.target[0][*]"}},
		{"evaluated expression text stays data", `{target: "${{ fromJSON('[\"\\u0024{{ ''safe'' }}\"]') }}"}`, nil},
		{"known insertion", `{target: [linux], "${{ insert }}": {extra: [safe]}}`, []string{"matrix.target", "matrix.extra"}},
		{"unknown insertion", `{target: [linux], "${{ insert }}": "${{ fromJSON(inputs.extra) }}"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: " + tc.matrix + "\n    steps:\n      - run: echo hello\n"
			workflow, errors := workflowlang.Parse([]byte(source))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			want := map[string]bool{}
			for _, path := range tc.paths {
				want[path] = true
			}
			if got := safeMatrixScriptReferences(workflow.Jobs["test"].Strategy.Matrix); !maps.Equal(got, want) {
				t.Fatalf("safe references = %v, want %v", got, want)
			}
		})
	}
}

func TestSafeMatrixScriptReferenceIndexes(t *testing.T) {
	proofs := map[string]bool{"matrix.container[*]": true, "matrix.nested[0][0]": true, "matrix.target.name": true}
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{"matrix.container[matrix.version]", true},
		{"matrix.container['3.10']", true},
		{"matrix.nested[0][0]", true},
		{"matrix.nested[0][inputs.index]", false},
		{"matrix['target']['NAME']", true},
		{"matrix['target.name']", false},
		{"matrix.container[inputs.key].name", false},
	} {
		node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := safeMatrixScriptReference(node, proofs); got != tc.want {
			t.Errorf("%s: safe=%t, want %t", tc.expression, got, tc.want)
		}
	}
}
