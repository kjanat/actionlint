package suppression

import (
	"strings"
	"testing"
)

func TestFlowStepSelectors(t *testing.T) {
	source := "jobs: {test: {steps: [{uses: ./a}, {uses: ./b}]}}"
	index := Contexts([]byte(source))
	for _, action := range []string{"./a", "./b"} {
		column := strings.Index(source, action) + 1
		for _, selector := range []string{"./a", "./b"} {
			if got := index.Match(1, column, "test", "", selector); got != (action == selector) {
				t.Errorf("finding %s matched selector %s: %v", action, selector, got)
			}
			if index.Match(1, 0, "test", "", selector) {
				t.Errorf("finding without a column matched ambiguous selector %s", selector)
			}
		}
	}
}

func TestNestedParallelScopes(t *testing.T) {
	source := `jobs:
  test:
    steps:
      - parallel:
          - id: first
            uses: ./a
          - parallel:
              - name: second
                uses: ./b
      - uses: ./c
`
	index := Contexts([]byte(source))
	for _, tc := range []struct {
		line       int
		step, uses string
	}{{6, "first", "./a"}, {9, "second", "./b"}, {10, "", "./c"}} {
		if !index.Match(tc.line, 20, "test", tc.step, tc.uses) {
			t.Errorf("did not match %s at line %d", tc.uses, tc.line)
		}
		if index.Match(tc.line, 20, "other", tc.step, tc.uses) {
			t.Errorf("matched wrong job at line %d", tc.line)
		}
	}
}

func TestFlowJobSelectors(t *testing.T) {
	source := "jobs: {a: {uses: ./one}, b: {uses: ./two}}"
	index := Contexts([]byte(source))
	column := strings.Index(source, "./two") + 1
	if !index.Match(1, column, "b", "", "./two") || index.Match(1, column, "a", "", "./one") {
		t.Fatal("job selectors crossed a flow-style sibling boundary")
	}
}

func TestSourcedBlockScope(t *testing.T) {
	index := Contexts([]byte("jobs:\n  test:\n    steps:\n      - id: script\n        run: |\n          echo hi\n      - run: echo bye\n"))
	if !index.Match(5, 0, "test", "script", "") {
		t.Fatal("unambiguous block header did not match without a column")
	}
	if index.Match(7, 20, "test", "script", "") {
		t.Fatal("preceding step captured next sibling")
	}
}
