package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCacheOperationCallerCeilings(t *testing.T) {
	for _, mode := range []string{"", "read", "write", "write-only", "none"} {
		for _, action := range []string{"actions/cache", "actions/cache/save", "actions/cache/restore"} {
			for _, prefix := range []string{"./", "$/"} {
				t.Run(mode+"/"+action+"/"+prefix, func(t *testing.T) {
					grant := ""
					if mode != "" {
						grant = "    cache-mode: " + mode + "\n"
					}
					caller := "on: push\njobs:\n  call:\n" + grant + "    uses: " + prefix + "callee.yaml\n"
					callee := "on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + action + "@v5\n        with: {key: test, path: cache}\n"
					want := mode == "none" || mode == "read" && action == "actions/cache/save" || mode == "write-only" && action == "actions/cache/restore"
					checkCacheOperationCalls(t, map[string]string{"caller.yaml": caller, "callee.yaml": callee}, "", func(t *testing.T, diagnostics []*Error) {
						if !want {
							if len(diagnostics) != 0 {
								t.Fatal(diagnostics)
							}
							return
						}
						line := 4
						if grant != "" {
							line++
						}
						if len(diagnostics) != 1 || diagnostics[0].Kind != "cache-operation" || filepath.Base(diagnostics[0].Filepath) != "caller.yaml" || diagnostics[0].Line != line || !strings.Contains(diagnostics[0].Message, `of "`+prefix+`callee.yaml"`) || !strings.Contains(diagnostics[0].Message, `caller-imposed cache-mode "`+mode+`"`) {
							t.Fatalf("want exactly one source-spelled call-site diagnostic: %v", diagnostics)
						}
					})
				})
			}
		}
	}
}

func TestCacheOperationCallerBoundaries(t *testing.T) {
	const save = "on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/cache/save@v5\n        with: {key: test, path: cache}\n"
	const caller = "on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml"
	for _, tc := range []struct {
		name, caller, callee, leaf, config string
		kind, file, fragment               string
	}{
		{name: "inline exception", caller: caller + " # actionlint:ignore cache-operation -- intentionally skip save\n", callee: save},
		{name: "disabled policy", caller: caller, callee: save, config: "policy: {cache-operation: false}"},
		{name: "explicit callee mode owns diagnostic", caller: caller, callee: "cache-mode: read\n" + save, kind: "cache-operation", file: "callee.yaml", fragment: "effective cache-mode"},
		{name: "escalation owns diagnostic", caller: caller, callee: "cache-mode: write\n" + save, kind: "workflow-call", file: "caller.yaml", fragment: "requests cache-mode"},
		{name: "nested inherited mode", caller: caller, callee: "on: workflow_call\njobs:\n  nested:\n    uses: $/leaf.yaml\n", leaf: save, kind: "cache-operation", file: "caller.yaml", fragment: `of "$/leaf.yaml"`},
		{name: "nested explicit ceiling", caller: "on: push\njobs:\n  call:\n    uses: $/callee.yaml\n", callee: "on: workflow_call\njobs:\n  nested:\n    cache-mode: read\n    uses: $/leaf.yaml\n", leaf: save, kind: "cache-operation", file: "caller.yaml", fragment: `of "$/leaf.yaml"`},
		{name: "cycle", caller: caller, callee: "on: workflow_call\njobs:\n  recurse:\n    uses: $/callee.yaml\n"},
		{name: "remote", caller: strings.ReplaceAll(caller, "$/callee.yaml", "owner/repo/.github/workflows/callee.yaml@main")},
		{name: "missing", caller: caller, kind: "workflow-call", file: "caller.yaml", fragment: "could not read reusable workflow"},
		{name: "malformed", caller: caller, callee: "on: [", kind: "workflow-call", file: "caller.yaml", fragment: "error while parsing reusable workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"caller.yaml": tc.caller}
			if tc.callee != "" {
				files["callee.yaml"] = tc.callee
			}
			if tc.leaf != "" {
				files["leaf.yaml"] = tc.leaf
			}
			checkCacheOperationCalls(t, files, tc.config, func(t *testing.T, diagnostics []*Error) {
				if tc.kind == "" {
					if len(diagnostics) != 0 {
						t.Fatal(diagnostics)
					}
					return
				}
				// The explicit callee restriction is diagnosed when linting that
				// file itself; it must not be repeated on its caller.
				if tc.file == "callee.yaml" {
					if len(diagnostics) != 0 {
						t.Fatal(diagnostics)
					}
					local := lintCachePolicy(t, tc.callee, tc.config)
					if len(local) != 1 || local[0].Kind != tc.kind || !strings.Contains(local[0].Message, tc.fragment) {
						t.Fatalf("expected only the callee's own cache-operation diagnostic: %v", local)
					}
					return
				}
				if len(diagnostics) != 1 || diagnostics[0].Kind != tc.kind || filepath.Base(diagnostics[0].Filepath) != tc.file || !strings.Contains(diagnostics[0].Message, tc.fragment) {
					t.Fatalf("want only %s at %s containing %q: %v", tc.kind, tc.file, tc.fragment, diagnostics)
				}
			})
		})
	}
}

func TestCacheOperationDistinctCallerCeilings(t *testing.T) {
	root := t.TempDir()
	callee := "on: workflow_call\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/cache/save@v5\n        with: {key: test, path: cache}\n"
	files := map[string]string{
		"callee.yaml": callee,
		"read.yaml":   "on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml\n",
		"write.yaml":  "on: push\ncache-mode: write\njobs:\n  call:\n    uses: $/callee.yaml\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := &Project{root: root}
	l, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]string{{"read.yaml", "write.yaml", "callee.yaml"}, {"callee.yaml", "write.yaml", "read.yaml"}} {
		cache := NewLocalReusableWorkflowCache(project, root, nil)
		for range 2 {
			for _, name := range order {
				diagnostics, err := l.check(name, []byte(files[name]), project, nil, nil, cache)
				if err != nil {
					t.Fatal(err)
				}
				if name == "read.yaml" {
					if len(diagnostics) != 1 || diagnostics[0].Kind != "cache-operation" {
						t.Fatal(diagnostics)
					}
				} else if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
			}
		}
		paths := make([]string, 0, len(order))
		for _, name := range order {
			paths = append(paths, filepath.Join(root, name))
		}
		diagnostics, err := l.LintFiles(paths, project)
		if err != nil {
			t.Fatal(err)
		}
		if len(diagnostics) != 1 || diagnostics[0].Kind != "cache-operation" || filepath.Base(diagnostics[0].Filepath) != "read.yaml" {
			t.Fatal(diagnostics)
		}
	}
}

// Force both ways of filling the shared cache before linting the caller. The
// metadata itself must be identical, and per-caller policy results stay uncached.
func checkCacheOperationCalls(t *testing.T, sources map[string]string, config string, check func(*testing.T, []*Error)) {
	t.Helper()
	root := t.TempDir()
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := &Project{root: root}
	l, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
	if err != nil {
		t.Fatal(err)
	}
	if config != "" {
		l.defaultConfig, err = ParseConfig([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, astFirst := range []bool{false, true} {
		cache := NewLocalReusableWorkflowCache(project, root, nil)
		if astFirst {
			for name, source := range sources {
				w, errs := Parse([]byte(source))
				if w == nil || len(errs) != 0 {
					continue
				}
				event, found := w.FindWorkflowCallEvent()
				if !found {
					continue
				}
				cache.WriteWorkflowCallEventFromWorkflow(name, event, w)
				fromAST, _, _ := cache.readCache("./" + name)
				fromFile, err := parseReusableWorkflowMetadata([]byte(source))
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
					t.Fatal(diff)
				}
			}
		}
		for range 2 {
			diagnostics, err := l.check("caller.yaml", []byte(sources["caller.yaml"]), project, nil, nil, cache)
			if err != nil {
				t.Fatal(err)
			}
			check(t, diagnostics)
		}
	}
}
