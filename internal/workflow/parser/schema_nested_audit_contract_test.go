package parser_test

import (
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func TestSchemaNestedDisabledService(t *testing.T) {
	src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      disabled:
        image: ''
    steps:
      - run: echo test
`
	_, errs := workflowparser.ParseDocument([]byte(src))
	if len(errs) != 0 {
		t.Fatalf("documented empty service image rejected: %v", errs)
	}
}

func TestSchemaNestedQuotedScalarTags(t *testing.T) {
	for _, field := range []string{
		"cancel-timeout-minutes: !!int \"300\"",
		"timeout-minutes: !!float \"3.0\"",
		"continue-on-error: !!bool \"true\"",
		"name: !!int \"300\"",
	} {
		t.Run(field, func(t *testing.T) {
			_, errs := workflowparser.ParseDocument([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    ` + field + `
    steps:
      - run: echo test
`))
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "tag of a quoted or block scalar") {
				t.Fatalf("wanted scalar tag/style diagnostic, got %v", errs)
			}
		})
	}
}

func TestSchemaNestedScheduleRequiresCron(t *testing.T) {
	src := `on:
  schedule:
    - cron: '0 0 * * *'
    - timezone: Europe/Amsterdam
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`
	_, errs := workflowparser.ParseDocument([]byte(src))
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `"cron" is missing`) || errs[0].Start.Line != 4 {
		t.Fatalf("wanted missing cron diagnostic at second schedule entry, got %v", errs)
	}
}

func TestSchemaNestedWaitAllStaticValue(t *testing.T) {
	for _, value := range []struct {
		text  string
		valid bool
	}{{"", true}, {"true", true}, {"True", true}, {"false", false}, {"${{ true }}", false}, {"${{ fromJSON('true') }}", false}, {"${{ github.event_name == 'push' }}", false}} {
		t.Run(value.text, func(t *testing.T) {
			_, errs := workflowparser.ParseDocument([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - wait-all: ` + value.text + "\n"))
			if valid := len(errs) == 0; valid != value.valid {
				t.Fatalf("valid=%v, expected %v: %v", valid, value.valid, errs)
			}
		})
	}
}

func TestSchemaNestedWorkflowDescription(t *testing.T) {
	w, errs := workflowparser.ParseDocument([]byte(`description: Build reusable binaries
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if w.Description == nil || w.Description.Value != "Build reusable binaries" {
		t.Fatalf("description was not preserved: %#v", w.Description)
	}
}
