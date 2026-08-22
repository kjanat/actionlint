package actionlint

import (
	"strings"
	"testing"
)

func runRuleRequiredActions(t *testing.T, src string, required []string) []*Error {
	t.Helper()
	w, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	r := NewRuleRequiredActions()
	r.SetConfig(&Config{Policy: Policy{RequiredActions: required}})
	v := NewVisitor()
	v.AddPass(r)
	if err := v.Visit(w); err != nil {
		t.Fatal(err)
	}
	return r.Errs()
}

func TestRuleRequiredActionsSplitActionRef(t *testing.T) {
	tests := []struct {
		in   string
		name string
		ref  string
	}{
		{"actions/checkout@v5", "actions/checkout", "v5"},
		{"actions/checkout", "actions/checkout", ""},
		{"./.github/actions/scan", "./.github/actions/scan", ""},
		{"docker://alpine:3", "docker://alpine:3", ""},
		{"owner/repo/path@abc123", "owner/repo/path", "abc123"},
		{"a@b@c", "a", "b@c"},
		{"", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			name, ref := splitActionRef(tc.in)
			if name != tc.name || ref != tc.ref {
				t.Fatalf("wanted (%q, %q) but got (%q, %q)", tc.name, tc.ref, name, ref)
			}
		})
	}
}

func TestRuleRequiredActionsMatchNameCaseInsensitively(t *testing.T) {
	src := `on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: Actions/Checkout@v5
`

	tests := []struct {
		what     string
		required string
		want     int
	}{
		{"lower case name matches upper case use", "actions/checkout", 0},
		{"upper case name matches lower case pattern", "ACTIONS/CHECKOUT", 0},
		{"ref is matched case sensitively", "actions/checkout@V5", 1},
		{"ref matches with the same case", "actions/checkout@v5", 0},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			errs := runRuleRequiredActions(t, src, []string{tc.required})
			if len(errs) != tc.want {
				t.Fatalf("wanted %d errors but got %d: %v", tc.want, len(errs), errs)
			}
		})
	}
}

func TestRuleRequiredActionsGlobPatterns(t *testing.T) {
	src := `on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4.2.2
      - uses: github/codeql-action/analyze@v3
`

	tests := []struct {
		required string
		want     int
	}{
		{"actions/checkout@v4*", 0},
		{"actions/checkout@v5*", 1},
		{"github/codeql-action/*", 0},
		{"github/codeql-action/*@v3", 0},
		{"github/*", 1},
	}

	for _, tc := range tests {
		t.Run(tc.required, func(t *testing.T) {
			errs := runRuleRequiredActions(t, src, []string{tc.required})
			if len(errs) != tc.want {
				t.Fatalf("wanted %d errors but got %d: %v", tc.want, len(errs), errs)
			}
		})
	}
}

func TestRuleRequiredActionsFirstJobPos(t *testing.T) {
	w := &Workflow{Jobs: map[string]*Job{}}
	for i := range 8 {
		id := string(rune('a' + i))
		w.Jobs[id] = &Job{
			ID:  &String{Value: id, Pos: &Pos{Line: 2 + i, Col: 3}},
			Pos: &Pos{Line: 2 + i, Col: 3},
		}
	}

	for range 100 {
		p := firstJobPos(w)
		if p == nil {
			t.Fatal("no position was returned")
		}
		if p.String() != "line:2,col:3" {
			t.Fatalf("wanted line:2,col:3 but got %s", p)
		}
	}

	if p := firstJobPos(&Workflow{}); p != nil {
		t.Fatalf("wanted nil for a workflow without a job but got %s", p)
	}
}

func TestRuleRequiredActionsTwoRefsOfSameAction(t *testing.T) {
	src := `on: push

jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
  b:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
`

	for range 100 {
		if errs := runRuleRequiredActions(t, src, []string{"actions/checkout@v5"}); len(errs) != 0 {
			t.Fatalf("wanted no error but got %v", errs)
		}
	}

	want := `no step in this workflow uses action "actions/checkout@v6" which is required by the "required-actions" policy in actionlint.yaml. "actions/checkout@v4" at line:7,col:15 matches the action name but not the required ref`
	for range 100 {
		errs := runRuleRequiredActions(t, src, []string{"actions/checkout@v6"})
		if len(errs) != 1 {
			t.Fatalf("wanted exactly one error but got %v", errs)
		}
		if errs[0].Message != want {
			t.Fatalf("wanted message %q but got %q", want, errs[0].Message)
		}
		if p := errs[0].Line; p != 4 {
			t.Fatalf("wanted the error at line 4 but got line %d", p)
		}
	}
}

func TestRuleRequiredActionsWithoutConfig(t *testing.T) {
	src := `on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hello'
`

	w, parsed := Parse([]byte(src))
	if len(parsed) > 0 {
		t.Fatal(parsed)
	}
	r := NewRuleRequiredActions()
	v := NewVisitor()
	v.AddPass(r)
	if err := v.Visit(w); err != nil {
		t.Fatal(err)
	}
	if errs := r.Errs(); len(errs) > 0 {
		t.Fatalf("wanted no error without a config but got %v", errs)
	}
}

func TestRuleRequiredActionsInvalidActionPattern(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"actions/checkout", "", false},
		{"actions/checkout@v4*", "", false},
		{"actions/[checkout", "actions/[checkout", true},
		{"actions/checkout@[v4", "[v4", true},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			p, ok := invalidActionPattern(tc.in)
			if ok != tc.ok || p != tc.want {
				t.Fatalf("wanted (%q, %v) but got (%q, %v)", tc.want, tc.ok, p, ok)
			}
		})
	}
}

func TestRuleRequiredActionsReusableWorkflowCallOnly(t *testing.T) {
	src := `on: push

jobs:
  call:
    uses: ./.github/workflows/reusable.yaml
`

	errs := runRuleRequiredActions(t, src, []string{"actions/checkout"})
	if len(errs) > 0 {
		t.Fatalf("wanted no error for a workflow which runs no step of its own but got %v", errs)
	}
}

func TestRuleRequiredActionsExpressionInUses(t *testing.T) {
	src := `on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ${{ format('{0}/checkout@v5', 'actions') }}
`

	errs := runRuleRequiredActions(t, src, []string{"my-org/scan@v2"})
	if len(errs) > 0 {
		t.Fatalf("wanted no error when a \"uses:\" contains an expression but got %v", errs)
	}
}

func TestRuleRequiredActionsNestedInParallelStep(t *testing.T) {
	src := `on: push

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - parallel:
          - uses: actions/checkout@v5
`

	errs := runRuleRequiredActions(t, src, []string{"actions/checkout"})
	if len(errs) > 0 {
		t.Fatalf("wanted no error for an action nested in a \"parallel:\" group but got %v", errs)
	}

	errs = runRuleRequiredActions(t, src, []string{"my-org/scan@v2"})
	if len(errs) != 1 {
		t.Fatalf("wanted exactly one error but got %v", errs)
	}
	if want := `no step in this workflow uses action "my-org/scan@v2"`; !strings.Contains(errs[0].Message, want) {
		t.Fatalf("wanted message %q to contain %q", errs[0].Message, want)
	}
}
