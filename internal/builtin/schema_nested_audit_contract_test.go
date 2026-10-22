package builtin_test

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/builtin"
)

func TestSchemaNestedEmptyChoiceOption(t *testing.T) {
	src := `on:
  workflow_dispatch:
    inputs:
      selection:
        type: choice
        options: ['', selected]
        default: ''
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	w, errs := workflowlang.ParseDocument([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := builtin.NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatalf("valid empty choice option/default rejected: %v", errs)
	}
}

func TestSchemaNestedImageVersionFilters(t *testing.T) {
	for _, filters := range []string{
		`types: ready
    names: build-image
    versions: '1.*'`,
		`types: [created, ready, deleted]
    names: [build-image]
    versions: ['1.*']`,
	} {
		src := `on:
  image_version:
    ` + filters + `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
		w, errs := workflowlang.ParseDocument([]byte(src))
		if len(errs) != 0 {
			t.Fatalf("schema-supported image filters rejected: %v", errs)
		}
		rule := builtin.NewRuleEvents()
		if err := rule.VisitWorkflowPre(w); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("schema-supported activity types rejected: %v", errs)
		}
	}
}

func TestSchemaNestedImageVersionRejectsUnknownActivity(t *testing.T) {
	src := `on:
  image_version:
    types: unavailable
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	w, errs := workflowlang.ParseDocument([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := builtin.NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 1 || !strings.Contains(errs[0].Message, `invalid activity type "unavailable"`) {
		t.Fatalf("wanted invalid image activity diagnostic, got %v", errs)
	}
}

func TestSchemaNestedStackedPullRequests(t *testing.T) {
	w, errs := workflowlang.ParseDocument([]byte(`on:
  pull_request:
    types: stacked
  pull_request_target:
    types: stacked
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := builtin.NewRuleEvents()
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestSchemaNestedStepIDBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id    string
		valid bool
	}{{"_user", true}, {"__reserved", false}, {strings.Repeat("a", 99), true}, {strings.Repeat("a", 100), false}} {
		t.Run(tc.id, func(t *testing.T) {
			w, errs := workflowlang.ParseDocument([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: ` + tc.id + `
        run: echo test
`))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := builtin.NewRuleID()
			if err := rule.VisitJobPre(w.Jobs["test"]); err != nil {
				t.Fatal(err)
			}
			if err := rule.VisitStep(w.Jobs["test"].Steps[0]); err != nil {
				t.Fatal(err)
			}
			if valid := len(rule.Errs()) == 0; valid != tc.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tc.valid, rule.Errs())
			}
		})
	}
}
