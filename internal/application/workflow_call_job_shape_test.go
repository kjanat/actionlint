package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowCallMixedJobMetadataOrder(t *testing.T) {
	for _, field := range []struct{ key, value string }{
		{"runs-on", "ubuntu-latest"},
		{"steps", "[{run: echo ok}]"},
		{"environment", "production"},
		{"outputs", "{result: value}"},
		{"env", "{FLAG: value}"},
		{"defaults", "{run: {shell: bash}}"},
		{"timeout-minutes", "10"},
		{"continue-on-error", "false"},
		{"container", "ubuntu:latest"},
		{"services", "{redis: {image: redis:latest}}"},
		{"snapshot", "{image-name: test-image}"},
		{"cancel-timeout-minutes", "5"},
	} {
		for _, leaf := range []struct{ name, source string }{
			{"missing", ""},
			{"malformed", "on: ["},
			{"cache mismatch", `on: workflow_call
cache-mode: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`},
		} {
			t.Run(field.key+"/"+leaf.name, func(t *testing.T) {
				root := t.TempDir()
				caller := []byte(`on: push
cache-mode: read
jobs:
  call:
    uses: $/callee.yaml
`)
				callee := []byte(`on: workflow_call
jobs:
  mixed:
    uses: $/leaf.yaml
    ` + field.key + ": " + field.value + "\n")
				for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee} {
					if err := os.WriteFile(filepath.Join(root, name), source, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if leaf.source != "" {
					if err := os.WriteFile(filepath.Join(root, "leaf.yaml"), []byte(leaf.source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				project := workspace.Identity(root)
				request := analysisRequest{WorkingDir: root, Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
				var err error

				check := func(diagnostics []*findingmodel.Finding, includeCallee bool) {
					t.Helper()
					if !includeCallee {
						if len(diagnostics) != 0 {
							t.Fatalf("invalid job created a caller diagnostic: %v", diagnostics)
						}
						return
					}
					if len(diagnostics) != 1 || diagnostics[0].Rule != "syntax-check" || diagnostics[0].Start.Line != 5 || filepath.Base(diagnostics[0].Path) != "callee.yaml" || !strings.Contains(diagnostics[0].Message, fmt.Sprintf("%q is not available", field.key)) {
						t.Fatalf("expected only the mixed-job syntax error: %v", diagnostics)
					}
				}
				for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
					cache := newCapturedWorkflowCache(t, project.RootDir(), root, nil)
					for range 2 {
						for _, name := range order {
							source := caller
							if name == "callee.yaml" {
								source = callee
							}
							diagnostics, err := checkWithCaches(t, t.Context(), request, sourceUnit{Path: name, Content: source, Project: project, Config: project.Config()}, nil, nil, cache)
							if err != nil {
								t.Fatal(err)
							}
							check(diagnostics, name == "callee.yaml")
						}
					}
					if _, _, found := cache.Lookup("./leaf.yaml"); found {
						t.Fatal("invalid job populated leaf cache")
					}
					request.Sources = readFixtureSources(t, []string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}, project, nil)
					diagnostics, err := testFindings(runTestAnalysis(t.Context(), request))
					if err != nil {
						t.Fatal(err)
					}
					check(diagnostics, true)
				}
				workflow, _ := documentparse.Parse(callee)
				event, _ := workflow.FindWorkflowCallEvent()
				cache := newCapturedWorkflowCache(t, project.RootDir(), root, nil)
				cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				fromAST, _, _ := cache.Lookup("./callee.yaml")
				fromFile, err := calls.ParseMetadata(callee)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}
