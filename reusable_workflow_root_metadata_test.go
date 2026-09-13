package actionlint

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReusableWorkflowDuplicateRootCacheModeMetadataOrder(t *testing.T) {
	for _, tc := range []struct {
		key, first, second, leaf string
		wantMismatch             bool
	}{
		{"cache-mode", "read", "write", "read", false},
		{"cache-mode", "write", "read", "read", true},
		{"cache-mode", "read", "write", "write", true},
		{"!!str cache-mode", "read", "write", "read", false},
		{"!!str cache-mode", "write", "read", "read", true},
		{"!!str cache-mode", "read", "write", "write", true},
		{`!!str "cache-mode"`, "read", "write", "read", false},
		{`!!str "cache-mode"`, "write", "read", "read", true},
		{`!!str "cache-mode"`, "read", "write", "write", true},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s/leaf=%s", tc.key, tc.first, tc.second, tc.leaf), func(t *testing.T) {
			root := t.TempDir()
			project := &Project{root: root}
			caller := []byte("on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml\n")
			callee := fmt.Appendf(nil, "on: workflow_call\n%s: %s\ncache-mode: %s\njobs:\n  nested:\n    uses: $/leaf.yaml\n", tc.key, tc.first, tc.second)
			leaf := fmt.Appendf(nil, "on: workflow_call\ncache-mode: %s\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n", tc.leaf)
			for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee, "leaf.yaml": leaf} {
				if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workflow, diagnostics := Parse(callee)
			if len(diagnostics) != 1 || diagnostics[0].Kind != "syntax-check" || diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, `key "cache-mode" is duplicated`) {
				t.Fatalf("expected the duplicate root declaration diagnostic, got %v", diagnostics)
			}
			event, ok := workflow.FindWorkflowCallEvent()
			if !ok {
				t.Fatal("workflow_call event missing")
			}
			linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			var previousMetadata *ReusableWorkflowMetadata
			var previousDiagnostics []*Error
			for _, fromAST := range []bool{false, true} {
				cache := NewLocalReusableWorkflowCache(project, root, nil)
				if fromAST {
					cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
				}
				metadata, err := cache.FindMetadata("./callee.yaml")
				if err != nil {
					t.Fatalf("AST=%v: duplicate root declaration prevented metadata extraction: %v", fromAST, err)
				}
				if fromAST {
					if diff := cmp.Diff(previousMetadata.JobCacheAccess, metadata.JobCacheAccess); diff != "" {
						t.Fatalf("root cache metadata differs by population order: %s", diff)
					}
				}
				previousMetadata = metadata
				for pass := range 2 {
					errs, err := linter.check("caller.yaml", caller, project, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					want := 0
					if tc.wantMismatch {
						want = 1
						if len(errs) == 1 && (errs[0].Kind != "workflow-call" || errs[0].Line != 5 || !strings.Contains(errs[0].Message, `requests cache-mode "write" but the calling job allows "read"`)) {
							t.Fatalf("unexpected caller diagnostic: %v", errs)
						}
					}
					if len(errs) != want {
						t.Fatalf("AST=%v pass=%d: wanted %d caller diagnostics, got %v", fromAST, pass, want, errs)
					}
					if fromAST {
						if diff := cmp.Diff(previousDiagnostics, errs, cmp.AllowUnexported(Error{})); diff != "" {
							t.Fatalf("caller diagnostics differ by cache population order: %s", diff)
						}
					}
					previousDiagnostics = errs
				}
				errs, err := linter.check("callee.yaml", callee, project, nil, nil, cache)
				if err != nil {
					t.Fatal(err)
				}
				duplicates := 0
				for _, diagnostic := range errs {
					if diagnostic.Kind == "syntax-check" && strings.Contains(diagnostic.Message, `key "cache-mode" is duplicated`) {
						duplicates++
					}
				}
				if duplicates != 1 {
					t.Fatalf("AST=%v: expected one duplicate syntax diagnostic, got %v", fromAST, errs)
				}
			}
		})
	}
}

func TestReusableWorkflowInvalidRootKeyMetadataOrder(t *testing.T) {
	for _, entry := range []struct{ key, message string }{
		{"!!int cache-mode", `invalid value "cache-mode" for "!!int" tag`},
		{"!!bool cache-mode", `invalid value "cache-mode" for "!!bool" tag`},
		{"!invalid cache-mode", "tag of a YAML scalar must be one of"},
		{`!!int "cache-mode"`, "tag of a quoted or block scalar must be"},
		{"[cache-mode]", "expected scalar node for string value but found sequence node"},
		{"{cache-mode: ignored}", "expected scalar node for string value but found mapping node"},
		{"''", "string should not be empty"},
		{"<<", `GitHub Actions does not support YAML merge key "<<"`},
		{"!!int 1", `unexpected key "1"`},
		{"!!bool true", `unexpected key "true"`},
		{"!!float 1.5", `unexpected key "1.5"`},
		{"!!null null", `unexpected key "null"`},
	} {
		for _, tc := range []struct {
			invalid, valid, leaf string
			wantMismatch         bool
		}{
			{"read", "write", "read", true},
			{"write", "read", "read", false},
			{"write", "read", "write", true},
		} {
			t.Run(fmt.Sprintf("%s/%s/%s/leaf=%s", entry.key, tc.invalid, tc.valid, tc.leaf), func(t *testing.T) {
				root := t.TempDir()
				project := &Project{root: root}
				caller := []byte("on: push\ncache-mode: read\njobs:\n  call:\n    uses: $/callee.yaml\n")
				callee := fmt.Appendf(nil, "on: workflow_call\n%s: %s\ncache-mode: %s\njobs:\n  nested:\n    uses: $/leaf.yaml\n", entry.key, tc.invalid, tc.valid)
				leaf := fmt.Appendf(nil, "on: workflow_call\ncache-mode: %s\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n", tc.leaf)
				for name, source := range map[string][]byte{"caller.yaml": caller, "callee.yaml": callee, "leaf.yaml": leaf} {
					if err := os.WriteFile(filepath.Join(root, name), source, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				workflow, diagnostics := Parse(callee)
				if len(diagnostics) != 1 || diagnostics[0].Kind != "syntax-check" || diagnostics[0].Line != 2 || !strings.Contains(diagnostics[0].Message, entry.message) {
					t.Fatalf("expected only the invalid root key diagnostic, got %v", diagnostics)
				}
				event, ok := workflow.FindWorkflowCallEvent()
				if !ok {
					t.Fatal("workflow_call event missing")
				}
				linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
				if err != nil {
					t.Fatal(err)
				}
				var previousMetadata *ReusableWorkflowMetadata
				var previousDiagnostics []*Error
				for _, fromAST := range []bool{false, true} {
					cache := NewLocalReusableWorkflowCache(project, root, nil)
					if fromAST {
						cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, workflow)
					}
					for pass := range 2 {
						errs, err := linter.check("caller.yaml", caller, project, nil, nil, cache)
						if err != nil {
							t.Fatal(err)
						}
						want := 0
						if tc.wantMismatch {
							want = 1
							if len(errs) == 1 && (errs[0].Kind != "workflow-call" || errs[0].Line != 5 || !strings.Contains(errs[0].Message, `requests cache-mode "write" but the calling job allows "read"`)) {
								t.Fatalf("AST=%v pass=%d: unexpected caller diagnostic: %v", fromAST, pass, errs)
							}
						}
						if len(errs) != want {
							t.Fatalf("AST=%v pass=%d: wanted %d caller diagnostics, got %v", fromAST, pass, want, errs)
						}
						if fromAST {
							if diff := cmp.Diff(previousDiagnostics, errs, cmp.AllowUnexported(Error{})); diff != "" {
								t.Fatalf("caller diagnostics differ by cache population order: %s", diff)
							}
						}
						previousDiagnostics = errs
					}
					metadata, err := cache.FindMetadata("./callee.yaml")
					if err != nil || metadata == nil {
						t.Fatalf("AST=%v: invalid root key prevented metadata extraction: %v", fromAST, err)
					}
					if fromAST {
						if diff := cmp.Diff(previousMetadata.JobCacheAccess, metadata.JobCacheAccess); diff != "" {
							t.Fatalf("root cache metadata differs by population order: %s", diff)
						}
					}
					previousMetadata = metadata
					errs, err := linter.check("callee.yaml", callee, project, nil, nil, cache)
					if err != nil {
						t.Fatal(err)
					}
					var syntax []*Error
					for _, diagnostic := range errs {
						if diagnostic.Kind == "syntax-check" {
							syntax = append(syntax, diagnostic)
						}
					}
					if len(syntax) != 1 || syntax[0].Line != 2 || syntax[0].Message != diagnostics[0].Message {
						t.Fatalf("AST=%v: invalid key diagnostic changed: %v", fromAST, errs)
					}
				}
			})
		}
	}
}
