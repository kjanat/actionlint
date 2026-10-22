package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestWorkflowCallMetadataFailureSpelling(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		for _, prefix := range []string{"./", "$/", "$//"} {
			t.Run(fmt.Sprintf("malformed=%v/prefix=%s", malformed, prefix), func(t *testing.T) {
				root := t.TempDir()
				if malformed {
					if err := os.WriteFile(filepath.Join(root, "broken.yaml"), []byte("on: ["), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				proj := workspace.Identity(root)
				cache := calls.NewCache(proj.RootDir(), root, nil)
				request := analysisRequest{WorkingDir: root}
				var err error

				uses := prefix + "broken.yaml"
				source := []byte(`on: push
jobs:
  call:
    uses: ` + uses + `
    with: {arg: '${{ github.ref }}'}
  consumer:
    needs: call
    runs-on: ubuntu-latest
    steps:
      - run: printf '%s\n' "$RESULT"
        env: {RESULT: '${{ needs.call.outputs.result }}'}
`)
				for _, pass := range []string{"cold cache", "warm cache"} {
					errs, err := checkWithCaches(t.Context(), request, sourceUnit{Path: "caller.yaml", Content: source, Project: proj, Config: proj.Config()}, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					if len(errs) != 1 || errs[0].Rule != "workflow-call" || errs[0].Start.Line != 4 || errs[0].Start.Column != 11 {
						t.Fatalf("%s: want one failure at the uses value, got %v", pass, errs)
					}
					if !strings.Contains(errs[0].Message, fmt.Sprintf("%q", uses)) {
						t.Fatalf("%s: original reference lost: %v", pass, errs)
					}
				}
				request.Sources = []sourceUnit{{Path: "caller.yaml", InputPath: "caller.yaml", Content: source, Project: proj, Config: proj.Config()}}
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				if len(errs) != 1 || errs[0].Start.Line != 4 || errs[0].Start.Column != 11 || !strings.Contains(errs[0].Message, fmt.Sprintf("%q", uses)) {
					t.Fatalf("Lint: want one failure using the source reference, got %v", errs)
				}
				// Rendering a caller's spelling must not alter the cached error.
				_, err = cache.FindMetadata("./broken.yaml")
				if err == nil || !strings.Contains(err.Error(), `"./broken.yaml"`) {
					t.Fatalf("cache no longer retains its lookup failure: %v", err)
				}
			})
		}
	}
}

func TestWorkflowCallInvalidJobKeyDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, leaf string
	}{
		{"cache mode", "CACHE-MODE", "write", ""},
		{"permissions", "PERMISSIONS", "{contents: write}", ""},
		{"missing unintended callee", "USES", "./leaf.yaml", ""},
		{"malformed unintended callee", "USES", "./leaf.yaml", "on: ["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			caller := []byte(`on: push
cache-mode: read
permissions: {contents: read}
jobs:
  call:
    uses: ./callee.yaml
`)
			callee := []byte(`on: workflow_call
jobs:
  build:
    ` + tc.key + ": " + tc.value + `
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`)
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.leaf != "" {
				if err := os.WriteFile(filepath.Join(root, "leaf.yaml"), []byte(tc.leaf), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			project := workspace.Identity(root)
			request := analysisRequest{WorkingDir: root, Tools: (adapters.Options{ShellCheck: ""}).Registrations()}

			checkDiagnostics := func(diagnostics []*findingmodel.Finding, includeCallee bool) {
				t.Helper()
				if !includeCallee {
					if len(diagnostics) != 0 {
						t.Fatalf("invalid callee key affected caller: %v", diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 || diagnostics[0].Rule != "syntax-check" || diagnostics[0].Start.Line != 4 || diagnostics[0].Start.Column != 5 || filepath.Base(diagnostics[0].Path) != "callee.yaml" || !strings.Contains(diagnostics[0].Message, fmt.Sprintf("unexpected key %q", tc.key)) {
					t.Fatalf("expected only the invalid key at callee.yaml:4:5, got %v", diagnostics)
				}
			}
			for _, order := range [][]string{{"caller.yaml", "callee.yaml"}, {"callee.yaml", "caller.yaml"}} {
				cache := calls.NewCache(project.RootDir(), root, nil)
				for range 2 {
					for _, name := range order {
						source := caller
						if name == "callee.yaml" {
							source = callee
						}
						diagnostics, err := checkWithCaches(t.Context(), request, sourceUnit{Path: name, Content: source, Project: project, Config: project.Config()}, nil, nil, cache)
						if err != nil {
							t.Fatal(err)
						}
						checkDiagnostics(diagnostics, name == "callee.yaml")
					}
				}
				paths := []string{filepath.Join(root, order[0]), filepath.Join(root, order[1])}
				request.Sources = readFixtureSources(t, paths, project, nil)
				diagnostics, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(diagnostics, true)
			}
		})
	}
}
