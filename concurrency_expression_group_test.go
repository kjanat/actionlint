package actionlint

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestConcurrencyExpressionGroups(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		emptyScalar bool
		emptyGroup  bool
		object      bool
	}{
		{name: "empty string", value: "${{ '' }}", emptyScalar: true},
		{name: "null", value: "${{ null }}", emptyScalar: true},
		{name: "JSON empty string", value: `${{ fromJSON('""') }}`, emptyScalar: true},
		{name: "JSON null", value: "${{ fromJSON('null') }}", emptyScalar: true},
		{name: "nonempty string", value: "${{ 'build' }}"},
		{name: "number conversion", value: "${{ 1 }}"},
		{name: "boolean conversion", value: "${{ false }}"},
		{name: "interpolated empty string", value: "prefix-${{ '' }}"},
		{name: "quoted whitespace", value: " ${{ '' }} "},
		{name: "unknown string", value: "${{ vars.GROUP }}"},
		{name: "unknown mapping", value: "${{ fromJSON(vars.CONCURRENCY) }}"},
		{name: "mapping", value: `${{ fromJSON('{"group":"build"}') }}`, object: true},
		{name: "empty mapping group", value: `${{ fromJSON('{"group":""}') }}`, emptyGroup: true, object: true},
		{name: "null mapping group", value: `${{ fromJSON('{"group":null}') }}`, emptyGroup: true, object: true},
	} {
		for _, scope := range []string{"workflow", "job"} {
			for _, form := range []string{"value", "group"} {
				if tc.object && form == "group" {
					continue
				}
				t.Run(scope+"/"+form+"/"+tc.name, func(t *testing.T) {
					field := fmt.Sprintf("concurrency: %q\n", tc.value)
					if form == "group" {
						field = fmt.Sprintf("concurrency: {group: %q}\n", tc.value)
					}
					source := "on: push\n"
					if scope == "workflow" {
						source += field
					}
					source += "jobs:\n  test:\n    runs-on: ubuntu-latest\n"
					if scope == "job" {
						source += "    " + field
					}
					source += "    steps:\n      - run: echo ok\n"
					lint, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
					if err != nil {
						t.Fatal(err)
					}
					errs, err := lint.Lint("test.yaml", []byte(source), nil)
					if err != nil {
						t.Fatal(err)
					}
					// The workflow schema permits an empty scalar; job concurrency
					// and both mapping forms require a nonempty group.
					if tc.emptyGroup || (tc.emptyScalar && (scope == "job" || form == "group")) {
						if len(errs) != 1 || errs[0].Kind != "expression" || !strings.Contains(errs[0].Message, "must be a non-empty string") {
							t.Fatalf("want one nonempty-group diagnostic, got %v", errs)
						}
					} else if len(errs) != 0 {
						t.Fatalf("unexpected diagnostics: %v", errs)
					}
				})
			}
		}
	}
}
