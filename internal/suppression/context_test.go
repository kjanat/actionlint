package suppression

import (
	"strings"
	"testing"
)

func TestFlowStepSelectors(t *testing.T) {
	source := "jobs: {test: {steps: [{uses: ./a}, {uses: ./b}]}}"
	index := Contexts([]byte(source), nil)
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
	index := Contexts([]byte(source), nil)
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
	index := Contexts([]byte(source), nil)
	column := strings.Index(source, "./two") + 1
	if !index.Match(1, column, "b", "", "./two") || index.Match(1, column, "a", "", "./one") {
		t.Fatal("job selectors crossed a flow-style sibling boundary")
	}
}

func TestSourcedBlockScope(t *testing.T) {
	index := Contexts([]byte("jobs:\n  test:\n    steps:\n      - id: script\n        run: |\n          echo hi\n      - run: echo bye\n"), nil)
	if !index.Match(5, 0, "test", "script", "") {
		t.Fatal("unambiguous block header did not match without a column")
	}
	if index.Match(7, 20, "test", "script", "") {
		t.Fatal("preceding step captured next sibling")
	}
}

func TestSourcedFirstStepKeyBoundary(t *testing.T) {
	index := Contexts([]byte("jobs:\n  test:\n    steps:\n      - run: echo first\n        id: first\n      - run: . script.sh\n        id: second\n"), nil)
	if !index.Match(6, 0, "test", "second", "") {
		t.Fatal("preceding range excluded the new step's run key")
	}
	if index.Match(6, 0, "test", "first", "") {
		t.Fatal("preceding step captured its sibling's run key")
	}
}

func TestSourcedMultilineFlowBoundary(t *testing.T) {
	source := "jobs: {test: {steps: [{id: first,\nrun: echo first}, {id: second, run: echo second}]}}"
	index := Contexts([]byte(source), nil)
	for _, step := range []string{"first", "second"} {
		if index.Match(2, 0, "test", step, "") {
			t.Fatal("ambiguous shared flow line matched", step)
		}
	}
}

func TestContextsUseYAMLSourceLines(t *testing.T) {
	source := "\uFEFFjobs:\r\n  test:\r\n    steps:\r\n      - uses: ./a\r\n"
	index := Contexts([]byte(source), nil)
	if !index.Match(4, 9, "test", "", "./a") {
		t.Fatalf("BOM or CRLF shifted the step's source position: %#v", index)
	}
}

func TestSuppressionYAMLLineEndings(t *testing.T) {
	for _, ending := range []struct {
		name string
		text string
	}{
		{name: "CR", text: "\r"},
		{name: "CRLF", text: "\r\n"},
		{name: "NEL", text: "\u0085"},
		{name: "LS", text: "\u2028"},
		{name: "PS", text: "\u2029"},
	} {
		t.Run(ending.name, func(t *testing.T) {
			source := []byte("\ufeffjobs:" + ending.text +
				"  test:" + ending.text +
				"    steps:" + ending.text +
				"      - run: echo hi # actionlint:ignore rule -- reason" + ending.text)
			if index := Contexts(source, nil); !index.Match(4, 9, "test", "", "") {
				t.Fatalf("step context lost source line for separator %q: %#v", ending.text, index)
			}
			directives, findings := Parse(source, []string{"rule"})
			if len(findings) != 0 || len(directives) != 1 {
				t.Fatalf("directives=%#v findings=%#v", directives, findings)
			}
			if directives[0].Position.Line != 4 || directives[0].First != 4 {
				t.Errorf("directive source position = line %d, target %d; want 4", directives[0].Position.Line, directives[0].First)
			}
		})
	}
}
