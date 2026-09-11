package actionlint

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowCallCacheModeNestedMetadataErrors(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%v", malformed), func(t *testing.T) {
			root := t.TempDir()
			caller := []byte("on: push\ncache-mode: read\njobs:\n  call:\n    uses: ./middle.yaml\n")
			middle := []byte("on: workflow_call\njobs:\n  nested:\n    uses: ./leaf.yaml\n")
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
			proj := &Project{root: root}
			l, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			cache := NewLocalReusableWorkflowCache(proj, root, nil)
			errs, err := l.check("caller.yaml", caller, proj, nil, nil, cache)
			if err != nil || len(errs) != 1 || !strings.Contains(errs[0].Message, "could not validate cache access through job") {
				t.Fatalf("nested validation did not report the callee failure: errors=%v, err=%v", errs, err)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					errs, err := l.check("middle.yaml", middle, proj, nil, nil, cache)
					if err != nil {
						t.Error(err)
						return
					}
					if len(errs) != 1 || errs[0].Kind != "workflow-call" || errs[0].Line != 4 || !strings.Contains(errs[0].Message, "./leaf.yaml") {
						t.Errorf("direct validation lost the callee error after nested lookup: %v", errs)
					}
				})
			}
			wg.Wait()
			for _, names := range [][]string{{"caller.yaml", "middle.yaml"}, {"middle.yaml", "caller.yaml"}} {
				paths := []string{filepath.Join(root, names[0]), filepath.Join(root, names[1])}
				errs, err := l.LintFiles(paths, proj)
				if err != nil {
					t.Fatal(err)
				}
				if len(errs) != 2 {
					t.Fatalf("LintFiles(%v): wanted errors at both call sites, got %v", names, errs)
				}
				seen := map[string]int{}
				for _, diagnostic := range errs {
					if diagnostic.Kind != "workflow-call" || !strings.Contains(diagnostic.Message, "./leaf.yaml") {
						t.Errorf("unexpected diagnostic: %v", diagnostic)
					}
					seen[filepath.Base(diagnostic.Filepath)]++
				}
				if seen["caller.yaml"] != 1 || seen["middle.yaml"] != 1 {
					t.Errorf("LintFiles(%v): unexpected diagnostic locations %v", names, seen)
				}
			}
		})
	}
}

func checkCacheModeCall(t *testing.T, caller string, callees map[string]string, fromAST bool) []*Error {
	t.Helper()
	root := t.TempDir()
	cache := NewLocalReusableWorkflowCache(&Project{root, nil}, root, nil)
	for name, src := range callees {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		if fromAST {
			w, errs := Parse([]byte(src))
			if len(errs) != 0 {
				t.Fatalf("%s: %v", name, errs)
			}
			event, ok := w.FindWorkflowCallEvent()
			if !ok {
				t.Fatalf("%s has no workflow_call event", name)
			}
			cache.WriteWorkflowCallEventFromWorkflow(name, event, w)
		}
	}
	w, errs := Parse([]byte(caller))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := NewRuleWorkflowCall("caller.yaml", cache)
	if err := rule.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	for _, j := range w.Jobs {
		if err := rule.VisitJobPre(j); err != nil {
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
					caller += "jobs:\n  call:\n    uses: ./callee.yaml\n"
					callee := "on: workflow_call\ncache-mode: " + calleeMode + "\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
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
					if len(errs) != 1 || errs[0].Message != want || errs[0].Line != 5 || errs[0].Column != 11 || errs[0].Kind != "workflow-call" {
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
				caller := "on: push\ncache-mode: " + tt.workflowMode + "\njobs:\n  call:\n    uses: $/callee.yaml\n"
				if tt.jobMode != "" {
					caller += "    cache-mode: " + tt.jobMode + "\n"
				}
				callee := "on: workflow_call\n"
				if tt.calleeMode != "" {
					callee += "cache-mode: " + tt.calleeMode + "\n"
				}
				callee += "jobs:\n  build:\n    runs-on: ubuntu-latest\n"
				if tt.calleeJobMode != "" {
					callee += "    cache-mode: " + tt.calleeJobMode + "\n"
				}
				callee += "    steps:\n      - run: echo ok\n"
				errs := checkCacheModeCall(t, caller, map[string]string{"callee.yaml": callee}, fromAST)
				if (len(errs) != 0) != tt.wantError {
					t.Fatalf("wantError=%v, got %v", tt.wantError, errs)
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
				caller += "jobs:\n  call:\n    uses: ./middle.yaml\n"
				middle := "on: workflow_call\n"
				if tt.middleMode != "" {
					middle += "cache-mode: " + tt.middleMode + "\n"
				}
				middle += "jobs:\n  nested:\n    uses: $/leaf.yaml\n"
				leaf := "on: workflow_call\ncache-mode: " + tt.leafMode + "\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle, "leaf.yaml": leaf}, fromAST)
				if (len(errs) != 0) != tt.wantError {
					t.Fatalf("wantError=%v, got %v", tt.wantError, errs)
				}
				if tt.wantError && (len(errs) != 1 || !strings.Contains(errs[0].Message, `of "./leaf.yaml"`)) {
					t.Fatalf("wanted one diagnostic identifying the nested callee, got %v", errs)
				}
			})
		}
	}
}

func TestWorkflowCallCacheModeCycleAndUnknownCallee(t *testing.T) {
	caller := "on: push\ncache-mode: read\njobs:\n  call:\n    uses: ./middle.yaml\n"
	for _, uses := range []string{"./middle.yaml", "owner/repo/.github/workflows/remote.yaml@main"} {
		middle := "on: workflow_call\njobs:\n  nested:\n    uses: " + uses + "\n"
		if errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle}, false); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
}

func TestWorkflowCallCacheModeSharedCallee(t *testing.T) {
	caller := "on: push\ncache-mode: write\njobs:\n  call:\n    uses: ./middle.yaml\n"
	middle := "on: workflow_call\njobs:\n  allowed:\n    cache-mode: write\n    uses: ./leaf.yaml\n  denied:\n    cache-mode: read\n    uses: ./leaf.yaml\n  duplicate:\n    cache-mode: read\n    uses: ./leaf.yaml\n"
	leaf := "on: workflow_call\ncache-mode: write\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	errs := checkCacheModeCall(t, caller, map[string]string{"middle.yaml": middle, "leaf.yaml": leaf}, false)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `calling job allows "read"`) {
		t.Fatalf("wanted one rejection of read-only access, got %v", errs)
	}
}

func TestReusableWorkflowCacheModeMetadataParity(t *testing.T) {
	src := []byte("on: workflow_call\ncache-mode: &mode read\njobs:\n  base: &job\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n  alias: *job\n  save:\n    cache-mode: write-only\n    uses: $/next.yaml\n  none:\n    cache-mode: none\n    uses: ./next.yaml\n  invalid:\n    cache-mode: null\n    uses: ./next.yaml\n  scalar-alias:\n    cache-mode: *mode\n    uses: ./next.yaml\n")
	fromFile, err := parseReusableWorkflowMetadata(src)
	if err != nil {
		t.Fatal(err)
	}
	w, errs := Parse(src)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "cache-mode") {
		t.Fatalf("expected the invalid null declaration, got %v", errs)
	}
	root := t.TempDir()
	cache := NewLocalReusableWorkflowCache(&Project{root, nil}, root, nil)
	event, _ := w.FindWorkflowCallEvent()
	cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, w)
	fromAST, _, ok := cache.readCache("./callee.yaml")
	if !ok {
		t.Fatal("no metadata was cached")
	}
	if diff := cmp.Diff(fromFile.JobCacheAccess, fromAST.JobCacheAccess); diff != "" {
		t.Fatal(diff)
	}
	for id, want := range map[string]CacheModeKind{"base": CacheModeRead, "alias": CacheModeRead, "save": CacheModeWriteOnly, "none": CacheModeNone, "invalid": CacheModeInvalid, "scalar-alias": CacheModeRead} {
		if mode := fromFile.JobCacheAccess[id].Mode; mode == nil || mode.Kind != want {
			t.Errorf("%s: wanted %v, got %v", id, want, mode)
		}
	}
}
