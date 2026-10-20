package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestWorkflowCallCacheModeNestedMetadataErrors(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%v", malformed), func(t *testing.T) {
			root := t.TempDir()
			caller := []byte(`on: push
cache-mode: read
jobs:
  call:
    uses: $/middle.yaml
`)
			middle := []byte(`on: workflow_call
jobs:
  nested:
    uses: $/leaf.yaml
`)
			if err := os.WriteFile(filepath.Join(root, "caller.yaml"), caller, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "middle.yaml"), middle, 0o600); err != nil {
				t.Fatal(err)
			}
			if malformed {
				if err := os.WriteFile(filepath.Join(root, "leaf.yaml"), []byte("on: ["), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			proj := workspace.Identity(root)
			request := analysisRequest{WorkingDir: root}
			var err error

			cache := calls.NewCache(proj.RootDir(), root, nil)
			errs, err := checkWithCaches(t.Context(), request, sourceUnit{Path: "caller.yaml", Content: caller, Project: proj, Config: proj.Config()}, nil, nil, cache)
			if err != nil || len(errs) != 1 || !strings.Contains(errs[0].Message, "could not validate cache access through job") {
				t.Fatalf("nested validation did not report the callee failure: errors=%v, err=%v", errs, err)
			}
			if !strings.Contains(errs[0].Message, `of "$/middle.yaml"`) || !strings.Contains(errs[0].Message, `"$/leaf.yaml"`) {
				t.Fatalf("nested lookup lost source spelling: %v", errs)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					errs, err := checkWithCaches(t.Context(), request, sourceUnit{Path: "middle.yaml", Content: middle, Project: proj, Config: proj.Config()}, nil, nil, cache)
					if err != nil {
						t.Error(err)
						return
					}
					if len(errs) != 1 || errs[0].Rule != "workflow-call" || errs[0].Start.Line != 4 || !strings.Contains(errs[0].Message, "$/leaf.yaml") {
						t.Errorf("direct validation lost the callee error after nested lookup: %v", errs)
					}
				})
			}
			wg.Wait()
			for _, names := range [][]string{{"caller.yaml", "middle.yaml"}, {"middle.yaml", "caller.yaml"}} {
				paths := []string{filepath.Join(root, names[0]), filepath.Join(root, names[1])}
				request.Sources = readFixtureSources(t, paths, proj, nil)
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				if len(errs) != 2 {
					t.Fatalf("LintFiles(%v): wanted errors at both call sites, got %v", names, errs)
				}
				seen := map[string]int{}
				for _, diagnostic := range errs {
					if diagnostic.Rule != "workflow-call" || !strings.Contains(diagnostic.Message, "$/leaf.yaml") {
						t.Errorf("unexpected diagnostic: %v", diagnostic)
					}
					seen[filepath.Base(diagnostic.Path)]++
				}
				if seen["caller.yaml"] != 1 || seen["middle.yaml"] != 1 {
					t.Errorf("LintFiles(%v): unexpected diagnostic locations %v", names, seen)
				}
			}
		})
	}
}
