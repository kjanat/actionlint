package workflow_test

import (
	"fmt"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestSchemaAuditScalarDecoding(t *testing.T) {
	for _, spelling := range []string{"true", "True", "TRUE"} {
		t.Run(spelling, func(t *testing.T) {
			workflow, errs := workflowlang.ParseDocument([]byte(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    continue-on-error: ` + spelling + `
    steps:
      - run: echo test
`))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			if value := workflow.Jobs["build"].ContinueOnError; value == nil || !value.Value {
				t.Fatalf("decoded true spelling as %#v", value)
			}
		})
	}
	for _, tc := range []struct {
		spelling string
		value    float64
	}{{"0x1e", 30}, {"0o36", 30}, {"030", 30}, {"3e1", 30}} {
		t.Run(tc.spelling, func(t *testing.T) {
			workflow, errs := workflowlang.ParseDocument(fmt.Appendf(nil, `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: %s
    steps:
      - run: echo test
`, tc.spelling))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			if value := workflow.Jobs["build"].TimeoutMinutes; value == nil || value.Value != tc.value {
				t.Fatalf("wanted %v, got %#v", tc.value, value)
			}
		})
	}
}

func TestSchemaAuditRequiredFlagsAreStatic(t *testing.T) {
	for _, event := range []string{
		`workflow_dispatch:
    inputs:
      value:
        type: string
        required: ${{ true }}`,
		`workflow_call:
    inputs:
      value:
        type: string
        required: ${{ true }}`,
		`workflow_call:
    secrets:
      value:
        required: ${{ true }}`,
	} {
		t.Run(event, func(t *testing.T) {
			_, errs := workflowlang.ParseDocument([]byte("on:\n  " + event + `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo test
`))
			if len(errs) == 0 {
				t.Fatal("accepted expression in a static boolean definition")
			}
		})
	}
}
