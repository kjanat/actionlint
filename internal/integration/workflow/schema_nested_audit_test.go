package workflow_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestSchemaNestedJobExpressions(t *testing.T) {
	for _, tc := range []struct {
		name, job, want string
	}{
		{"cancel timeout literal", "cancel-timeout-minutes: 2.5", ""},
		{"cancel timeout strategy context", "cancel-timeout-minutes: ${{ strategy.job-index }}", ""},
		{"cancel timeout forbidden context", "cancel-timeout-minutes: ${{ secrets.TIMEOUT }}", "secrets"},
		{"cancel timeout wrong type", "cancel-timeout-minutes: ${{ true }}", "must be number"},
		{"cancel timeout wrong shape", "cancel-timeout-minutes: []", "float value"},
		{"include expression object", `strategy:
      matrix:
        include:
          - ${{ fromJSON('{"os":"ubuntu-latest"}') }}`, ""},
		{"include expression scalar", `strategy:
      matrix:
        include:
          - ${{ true }}`, "object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error

			src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    ` + tc.job + `
    steps:
      - run: echo test
`
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(src), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted diagnostic containing %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestSchemaNestedStructuredExpressions(t *testing.T) {
	for _, tc := range []struct {
		name, field string
	}{
		{"strategy", "strategy: ${{ fromJSON('{\"matrix\":{\"os\":[\"ubuntu-latest\"]}}') }}"},
		{"defaults", "defaults:\n      run: ${{ fromJSON('{\"shell\":\"bash\"}') }}"},
		{"container", "container: ${{ fromJSON('{\"image\":\"ubuntu:latest\"}') }}"},
		{"service", "services:\n      redis: ${{ fromJSON('{\"image\":\"redis:latest\"}') }}"},
		{"environment", "environment: ${{ fromJSON('{\"name\":\"staging\"}') }}"},
		{"concurrency", "concurrency: ${{ fromJSON('{\"group\":\"deploy\",\"cancel-in-progress\":false}') }}"},
		{"queue", `concurrency:
      group: deploy
      queue: ${{ vars.QUEUE }}`},
		{"snapshot", "snapshot: ${{ fromJSON('{\"image-name\":\"build-image\"}') }}"},
		{"ports", `container:
      image: ubuntu:latest
      ports: ${{ fromJSON('["8080:80"]') }}`},
		{"volumes", `container:
      image: ubuntu:latest
      volumes: ${{ fromJSON('["/src:/src"]') }}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error

			src := `on: push
jobs:
  test:
    runs-on: ${{ fromJSON('{"labels":"ubuntu-latest"}') }}
    ` + tc.field + `
    steps:
      - uses: docker://alpine:3
        with: ${{ fromJSON('{"args":"echo test"}') }}
`
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(src), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil || len(errs) != 0 {
				t.Fatalf("schema-supported expression object rejected: %v %v", err, errs)
			}
		})
	}
}
