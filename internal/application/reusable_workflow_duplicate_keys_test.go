package application

import (
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

func TestReusableWorkflowDuplicateKeyMetadataOrder(t *testing.T) {
	for _, tc := range []struct{ name, jobs string }{
		{"cache-mode", `  call:
    uses: ./leaf.yaml
    cache-mode: read
    cache-mode: write
`},
		{"uses", `  call:
    uses: ./leaf.yaml
    uses: ./missing.yaml
`},
		{"permissions", `  call:
    uses: ./leaf.yaml
    permissions: {contents: read}
    permissions: {contents: write}
`},
		{"job", `  call:
    uses: ./leaf.yaml
  call:
    uses: ./missing.yaml
`},
		{"job casing", `  call:
    uses: ./leaf.yaml
  CALL:
    uses: ./missing.yaml
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			project := workspace.Identity(root)
			caller := []byte(`on: push
cache-mode: read
permissions: {contents: read}
jobs:
  call:
    uses: ./callee.yaml
`)
			callee := []byte(`on: workflow_call
cache-mode: read
jobs:
` + tc.jobs)
			leaf := []byte(`on: workflow_call
jobs:
  build:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`)
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee, "leaf.yaml": leaf} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow, errs := documentparse.Parse(callee)
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "is duplicated") {
				t.Fatalf("expected one duplicate key diagnostic, got %v", errs)
			}
			event, ok := workflow.FindWorkflowCallEvent()
			if !ok {
				t.Fatal("workflow_call event missing")
			}
			astCache := newCapturedWorkflowCache(t, project.RootDir(), root, nil)
			astCache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
			fromAST, _, _ := astCache.Lookup("./callee.yaml")
			fromFile, err := calls.ParseMetadata(callee)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(fromAST.JobPermissions, fromFile.JobPermissions); diff != "" {
				t.Fatal(diff)
			}
			request := analysisRequest{WorkingDir: root, Tools: (adapters.Options{ShellCheck: ""}).Registrations()}

			check := func(errs []*findingmodel.Finding, want int) {
				t.Helper()
				if len(errs) != want {
					t.Fatalf("wanted %d duplicate-key diagnostics, got %v", want, errs)
				}
				for _, err := range errs {
					if err.Rule != "syntax-check" || !strings.Contains(err.Message, "is duplicated") || filepath.Base(err.Path) != "callee.yaml" {
						t.Fatalf("unexpected diagnostic: %v", err)
					}
				}
			}
			for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
				cache := newCapturedWorkflowCache(t, project.RootDir(), root, nil)
				for _, name := range order {
					source, want := caller, 0
					if name == "callee.yaml" {
						source, want = callee, 1
					}
					errs, err := checkWithCaches(t, t.Context(), request, sourceUnit{Path: name, Content: source, Project: project, Config: project.Config()}, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					check(errs, want)
				}
				if _, _, found := cache.Lookup("./missing.yaml"); found {
					t.Fatal("discarded duplicate populated missing workflow cache")
				}
				request.Sources = readFixtureSources(t, []string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}, project, nil)
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				check(errs, 1)
			}
		})
	}
}
