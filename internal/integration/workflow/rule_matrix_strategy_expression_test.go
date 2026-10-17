package workflow_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRuleMatrixKnownStrategyExpression(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, want string
	}{
		{"duplicate strings", `{"os":["ubuntu-latest","ubuntu-latest"]}`, "duplicate value"},
		{"duplicate numbers", `{"version":[1,1.0]}`, "duplicate value"},
		{"duplicate objects", `{"value":[{"version":1},{"VERSION":1.0}]}`, "duplicate value"},
		{"distinct scalar types", `{"value":[1,"1",true,"true",null,"null"]}`, ""},
		{"unknown exclude axis", `{"os":["ubuntu-latest"],"exclude":[{"arch":"arm64"}]}`, `"arch" in "exclude" section does not exist`},
		{"unmatched exclude", `{"os":["ubuntu-latest"],"exclude":[{"os":"windows-latest"}]}`, "does not match"},
		{"exclude numeric string", `{"value":["1"],"exclude":[{"value":1}]}`, ""},
		{"exclude nonnumeric string", `{"value":["one"],"exclude":[{"value":1}]}`, "does not match"},
		{"exclude same number", `{"value":[1],"exclude":[{"value":1.0}]}`, ""},
		{"exclude object subset", `{"value":[{"os":"linux","arch":"arm64"}],"exclude":[{"value":{"OS":"linux"}}]}`, ""},
		{"exclude array", `{"value":[[1]],"exclude":[{"value":[1.0]}]}`, ""},
		{"exclude without variations", `{"exclude":[{"os":"linux"}]}`, "no matrix variation"},
		{"include does not extend exclude axes", `{"os":["linux"],"include":[{"arch":"arm64"}],"exclude":[{"arch":"arm64"}]}`, "does not exist"},
		{"include does not extend exclude values", `{"os":["linux"],"include":[{"os":"windows"}],"exclude":[{"os":"windows"}]}`, "does not match"},
		{"case insensitive keys", `{"OS":["linux"],"EXCLUDE":[{"os":"windows"}]}`, "does not match"},
		{"expression-looking row is data", `{"os":["${{ vars.RUNNER }}"],"exclude":[{"os":"linux"}]}`, "does not match"},
		{"expression-looking exclude is data", `{"os":["linux"],"exclude":[{"os":"${{ vars.RUNNER }}"}]}`, "does not match"},
		{"nested expression-looking data", `{"value":[{"os":"linux"}],"exclude":[{"value":{"os":"${{ vars.RUNNER }}"}}]}`, "does not match"},
		{"array expression-looking data", `{"value":[["${{ vars.RUNNER }}"]],"exclude":[{"value":["linux"]}]}`, "does not match"},
		{"identical expression-looking data", `{"os":["${{ vars.RUNNER }}"],"exclude":[{"os":"${{ vars.RUNNER }}"}]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    strategy: ${{ fromJSON('{"matrix":` + tc.matrix + `}') }}
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			request := application.Options{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
			var requestSources []workspace.Source
			var err error
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
			} else if len(errs) != 1 || errs[0].Rule != "matrix" || !strings.Contains(errs[0].Message, tc.want) || errs[0].Start.Line != 4 || errs[0].Start.Column != 15 || errs[0].Path != "test.yaml" {
				t.Fatalf("wanted one %q matrix diagnostic at test.yaml:4:15, got %v", tc.want, errs)
			}
		})
	}
}
