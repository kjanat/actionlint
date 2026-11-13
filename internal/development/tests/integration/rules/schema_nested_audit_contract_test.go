package rules_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestSchemaNestedEmptyChoiceOption(t *testing.T) {
	t.Parallel()
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
	w, errs := workflowparser.Parse([]byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := declarationchecks.New(checking.ConfigurationRules(), correctness.EventsCheck())
	if err := rule.VisitWorkflowPre(fixturedecl.Workflow(w)); err != nil {
		t.Fatal(err)
	}
	if errs := rule.Errs(); len(errs) != 0 {
		t.Fatalf("valid empty choice option/default rejected: %v", errs)
	}
}

func TestSchemaNestedImageVersionFilters(t *testing.T) {
	t.Parallel()
	for _, filters := range []string{
		`names: build-image
    versions: '1.*'`,
		`names: [build-image]
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
		w, errs := workflowparser.Parse([]byte(src))
		if len(errs) != 0 {
			t.Fatalf("schema-supported image filters rejected: %v", errs)
		}
		rule := declarationchecks.New(checking.ConfigurationRules(), correctness.EventsCheck())
		if err := rule.VisitWorkflowPre(fixturedecl.Workflow(w)); err != nil {
			t.Fatal(err)
		}
		if errs := rule.Errs(); len(errs) != 0 {
			t.Fatalf("documented image filters rejected: %v", errs)
		}
	}
}

func TestSchemaNestedImageVersionActivities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		activity string
		valid    bool
	}{{"created", true}, {"ready", true}, {"deleted", true}, {"unsupported", false}} {
		t.Run(tc.activity, func(t *testing.T) {
			t.Parallel()
			src := `on:
  image_version:
    types: ` + tc.activity + `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
			w, errs := workflowparser.Parse([]byte(src))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := declarationchecks.New(checking.ConfigurationRules(), correctness.EventsCheck())
			if err := rule.VisitWorkflowPre(fixturedecl.Workflow(w)); err != nil {
				t.Fatal(err)
			}
			if errs := rule.Errs(); tc.valid {
				if len(errs) != 0 {
					t.Fatalf("reviewed image activity rejected: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, `invalid activity type "unsupported"`) {
				t.Fatalf("wanted invalid image activity diagnostic, got %v", errs)
			}
		})
	}
}

func TestSchemaNestedStackedPullRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		activity string
		valid    bool
	}{{"stacked", true}, {"unsupported", false}} {
		t.Run(tc.activity, func(t *testing.T) {
			t.Parallel()
			w, errs := workflowparser.Parse([]byte(`on:
  pull_request:
    types: ` + tc.activity + `
  pull_request_target:
    types: ` + tc.activity + `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := declarationchecks.New(checking.ConfigurationRules(), correctness.EventsCheck())
			if err := rule.VisitWorkflowPre(fixturedecl.Workflow(w)); err != nil {
				t.Fatal(err)
			}
			if errs := rule.Errs(); tc.valid {
				if len(errs) != 0 {
					t.Fatalf("reviewed pull request activity rejected: %v", errs)
				}
			} else if len(errs) != 2 || !strings.Contains(errs[0].Message, `invalid activity type "unsupported"`) || !strings.Contains(errs[1].Message, `invalid activity type "unsupported"`) {
				t.Fatalf("wanted invalid pull request activity diagnostics, got %v", errs)
			}
		})
	}
}

func TestSchemaNestedStepIDBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id    string
		valid bool
	}{{"_user", true}, {"__reserved", false}, {strings.Repeat("a", 99), true}, {strings.Repeat("a", 100), false}} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			w, errs := workflowparser.Parse([]byte(`on: push
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
			rule := declarationchecks.New(checking.ConfigurationRules(), correctness.IdCheck())
			if err := rule.VisitJobPre(fixturedecl.Job(w.Jobs["test"])); err != nil {
				t.Fatal(err)
			}
			if err := rule.VisitStep(fixturedecl.Step(w.Jobs["test"].Steps[0])); err != nil {
				t.Fatal(err)
			}
			if valid := len(rule.Errs()) == 0; valid != tc.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, tc.valid, rule.Errs())
			}
		})
	}
}
