package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWorkflowLiteralUsesDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "action"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "name: test\ndescription: test\ninputs:\n  token:\n    description: token\n    required: true\nruns:\n  using: composite\n  steps: [{run: echo ok, shell: bash}]\n"
	if err := os.WriteFile(filepath.Join(root, "action", "action.yml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "malformed"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "malformed", "action.yml"), []byte("runs: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, uses, extra, want string }{
		{"missing action remains permitted", "./missing-action", "", ""},
		{"malformed metadata", "./malformed", "", "could not parse action metadata"},
		{"required input", "./action", "", "missing input"},
		{"provided input", "./action", "\n        with: {token: value}", ""},
		{"invalid spec", "not-an-action", "", "invalid"},
		{"known outputs", "actions/checkout@v6", "\n      - run: echo ${{ steps.action.outputs.missing }}", `property "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plain []*Error
			for _, uses := range []string{tc.uses, "${{ '" + tc.uses + "' }}"} {
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - id: action\n        uses: " + uses + tc.extra + "\n"
				linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, Shellcheck: "", Pyflakes: ""})
				if err != nil {
					t.Fatal(err)
				}
				diagnostics, err := linter.Lint("workflow.yml", []byte(source), &Project{root: root})
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" && len(diagnostics) != 0 || tc.want != "" && (len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, tc.want)) {
					t.Fatalf("wanted %q, got %v", tc.want, diagnostics)
				}
				if uses == tc.uses {
					plain = diagnostics
				} else if diff := cmp.Diff(plain, diagnostics, cmp.AllowUnexported(Error{})); diff != "" {
					t.Fatal(diff)
				}
			}
		})
	}
}

func TestWorkflowLiteralStaticValues(t *testing.T) {
	source := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - id: ${{ 'build' }}\n        run: echo ok\n        shell: ${{ 'bash' }}\n      - uses: ${{ 'docker://alpine:3' }}\n        with: {entrypoint: sh, args: -c}\n")
	w, errs := Parse(source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	steps := w.Jobs["test"].Steps
	if steps[0].ID.Value != "build" || steps[0].ID.Pos.Line != 6 || steps[0].Exec.(*ExecRun).Shell.Value != "bash" {
		t.Fatal("static values or positions changed")
	}
	action := steps[1].Exec.(*ExecAction)
	if action.Uses.Value != "docker://alpine:3" || action.Entrypoint == nil || action.Args == nil || len(action.Inputs) != 0 {
		t.Fatalf("Docker input dispatch lost: %#v", action)
	}
}

func TestWorkflowLiteralCallMetadata(t *testing.T) {
	source := []byte("on: workflow_call\njobs:\n  call:\n    uses: ${{ '$/leaf.yaml' }}\n")
	w, errs := Parse(source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	root := t.TempDir()
	cache := NewLocalReusableWorkflowCache(&Project{root: root}, root, nil)
	event, _ := w.FindWorkflowCallEvent()
	cache.WriteWorkflowCallEventFromWorkflow("callee.yaml", event, w)
	fromAST, _, _ := cache.readCache("./callee.yaml")
	fromFile, err := parseReusableWorkflowMetadata(source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(fromAST.JobCacheAccess, fromFile.JobCacheAccess); diff != "" {
		t.Fatal(diff)
	}
	if access := fromFile.JobCacheAccess["call"]; access.Uses != "./leaf.yaml" || access.SourceUses != "$/leaf.yaml" {
		t.Fatalf("literal call lost: %#v", access)
	}
}

func TestWorkflowLiteralCallInvalidSpec(t *testing.T) {
	for _, uses := range []string{"oooops", "${{ 'oooops' }}"} {
		linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
		if err != nil {
			t.Fatal(err)
		}
		errs, err := linter.Lint("workflow.yml", []byte("on: push\njobs:\n  call:\n    uses: "+uses+"\n"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 || errs[0].Kind != "workflow-call" || !strings.Contains(errs[0].Message, "not following the format") {
			t.Fatalf("invalid reference %q: %v", uses, errs)
		}
	}
}
