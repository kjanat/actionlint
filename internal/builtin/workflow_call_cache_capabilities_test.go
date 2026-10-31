package builtin_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func checkCacheModeCall(t *testing.T, caller string, callees map[string]string, fromAST bool) []*findingmodel.Finding {
	t.Helper()
	root := t.TempDir()
	cache, snapshots := newCapturedWorkflowCacheAndSnapshots(t, root, root, nil)
	for name, src := range callees {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		if fromAST {
			w, errs := workflowparser.ParseDocument([]byte(src))
			if len(errs) != 0 {
				t.Fatalf("%s: %v", name, errs)
			}
			_, ok := workflowlang.FindWorkflowCallEvent(w)
			if !ok {
				t.Fatalf("%s has no workflow_call event", name)
			}
			path := filepath.Join(root, name)
			snapshot, err := snapshots.Document(path, []byte(src), documentparse.Workflow(path, []byte(src)))
			if err != nil {
				t.Fatal(err)
			}
			if err := cache.Seed(snapshot); err != nil {
				t.Fatal(err)
			}
		}
	}
	w, errs := workflowparser.ParseDocument([]byte(caller))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := builtin.NewRuleWorkflowCall(documentparse.SourceSnapshot{}, cache)
	if err := rule.VisitWorkflowPre(ownedWorkflowFixture(w)); err != nil {
		t.Fatal(err)
	}
	for _, j := range w.Jobs {
		if err := rule.VisitJobPre(ownedJobFixture(j)); err != nil {
			t.Fatal(err)
		}
	}
	return rule.Errs()
}

func TestWorkflowCallCacheModeCapabilities(t *testing.T) {
	allowed := map[string][]string{
		"":           {"none", "read", "write", "write-only"},
		"none":       {"none"},
		"read":       {"none", "read"},
		"write-only": {"none", "write-only"},
		"write":      {"none", "read", "write", "write-only"},
	}
	for _, fromAST := range []bool{false, true} {
		for _, callerMode := range []string{"", "none", "read", "write-only", "write"} {
			for _, calleeMode := range []string{"none", "read", "write-only", "write"} {
				t.Run(fmt.Sprintf("AST=%v/%s/%s", fromAST, callerMode, calleeMode), func(t *testing.T) {
					caller := "on: pull_request_target\n"
					if callerMode != "" {
						caller += "cache-mode: " + callerMode + "\n"
					}
					caller += `jobs:
  call:
    uses: ./callee.yaml
`
					callee := "on: workflow_call\ncache-mode: " + calleeMode + `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
					errs := checkCacheModeCall(t, caller, map[string]string{"callee.yaml": callee}, fromAST)
					wantError := true
					for _, mode := range allowed[callerMode] {
						if mode == calleeMode {
							wantError = false
						}
					}
					if !wantError {
						if len(errs) != 0 {
							t.Fatal(errs)
						}
						return
					}
					want := fmt.Sprintf("nested job %q of %q requests cache-mode %q but the calling job allows %q", "build", "./callee.yaml", calleeMode, callerMode)
					if len(errs) != 1 || errs[0].Message != want || errs[0].Start.Line != 5 || errs[0].Start.Column != 11 || errs[0].Rule != "workflow-call" {
						t.Fatalf("wanted %q at 5:11, got %v", want, errs)
					}
				})
			}
		}
	}
}

func TestWorkflowCallCacheModeInheritance(t *testing.T) {
	tests := []struct {
		name, workflowMode, jobMode, calleeMode, calleeJobMode string
		wantError                                              bool
	}{
		{"caller job narrows", "write", "read", "write", "", true},
		{"caller job overrides none", "none", "write", "write", "", false},
		{"caller job disables", "write", "none", "read", "", true},
		{"callee job overrides", "read", "", "write", "read", false},
		{"callee job broadens", "read", "", "read", "write", true},
		{"callee job disables", "read", "", "write", "none", false},
		{"callee inherits caller", "none", "", "", "", false},
	}
	for _, tt := range tests {
		for _, fromAST := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/AST=%v", tt.name, fromAST), func(t *testing.T) {
				caller := "on: push\ncache-mode: " + tt.workflowMode + `
jobs:
  call:
    uses: $/callee.yaml
`
				if tt.jobMode != "" {
					caller += "    cache-mode: " + tt.jobMode + "\n"
				}
				callee := "on: workflow_call\n"
				if tt.calleeMode != "" {
					callee += "cache-mode: " + tt.calleeMode + "\n"
				}
				callee += `jobs:
  build:
    runs-on: ubuntu-latest
`
				if tt.calleeJobMode != "" {
					callee += "    cache-mode: " + tt.calleeJobMode + "\n"
				}
				callee += `    steps:
      - run: echo ok
`
				errs := checkCacheModeCall(t, caller, map[string]string{"callee.yaml": callee}, fromAST)
				if (len(errs) != 0) != tt.wantError {
					t.Fatalf("wantError=%v, got %v", tt.wantError, errs)
				}
				if tt.wantError && !strings.Contains(errs[0].Message, `of "$/callee.yaml"`) {
					t.Fatalf("direct cache diagnostic lost source spelling: %v", errs)
				}
			})
		}
	}
}

func TestWorkflowCallCacheModeNested(t *testing.T) {
	tests := []struct {
		name, callerMode, middleMode, leafMode string
		wantError                              bool
	}{
		{"inherited cap", "read", "", "write", true},
		{"nested read accepted", "read", "", "read", false},
		{"nested none accepted", "read", "", "none", false},
		{"nested cap narrows", "write", "read", "write", true},
		{"absent outer cap", "", "read", "write", true},
		{"absent caps", "", "", "write", false},
		{"save-only cannot restore", "write-only", "", "read", true},
	}
	for _, tt := range tests {
		for _, fromAST := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/AST=%v", tt.name, fromAST), func(t *testing.T) {
				caller := "on: push\n"
				if tt.callerMode != "" {
					caller += "cache-mode: " + tt.callerMode + "\n"
				}
				caller += `jobs:
  call:
    uses: ./middle.yaml
`
				middle := "on: workflow_call\n"
				if tt.middleMode != "" {
					middle += "cache-mode: " + tt.middleMode + "\n"
				}
				middle += `jobs:
  nested:
    uses: $/leaf.yaml
`
				leaf := "on: workflow_call\ncache-mode: " + tt.leafMode + `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle, "leaf.yaml": leaf}, fromAST)
				if (len(errs) != 0) != tt.wantError {
					t.Fatalf("wantError=%v, got %v", tt.wantError, errs)
				}
				if tt.wantError && (len(errs) != 1 || !strings.Contains(errs[0].Message, `of "$/leaf.yaml"`)) {
					t.Fatalf("wanted one diagnostic identifying the nested callee, got %v", errs)
				}
			})
		}
	}
}

func TestWorkflowCallCacheModeCycleAndUnknownCallee(t *testing.T) {
	caller := `on: push
cache-mode: read
jobs:
  call:
    uses: ./middle.yaml
`
	for _, uses := range []string{"./middle.yaml", "$/middle.yaml", "$//middle.yaml", "owner/repo/.github/workflows/remote.yaml@main"} {
		middle := `on: workflow_call
jobs:
  nested:
    uses: ` + uses + "\n"
		if errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle}, false); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
}

func TestWorkflowCallCacheModeSharedCallee(t *testing.T) {
	caller := `on: push
cache-mode: write
jobs:
  call:
    uses: ./middle.yaml
`
	middle := `on: workflow_call
jobs:
  allowed:
    cache-mode: write
    uses: ./leaf.yaml
  denied:
    cache-mode: read
    uses: ./leaf.yaml
  duplicate:
    cache-mode: read
    uses: $/leaf.yaml
`
	leaf := `on: workflow_call
cache-mode: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle, "leaf.yaml": leaf}, false)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `calling job allows "read"`) {
		t.Fatalf("wanted one rejection of read-only access, got %v", errs)
	}
}
