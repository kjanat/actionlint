package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"

	"actionlint.kjanat.dev/v2/internal/results/outline"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCommandActionInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const manifest = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ secrets.TOKEN }}\n      shell: bash\n"
	for name, content := range map[string]string{"action.yml": manifest, "workflow.yml": testutil.GoodWorkflow} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, modern := range []bool{false, true} {
		for _, stdin := range []bool{false, true} {
			args := []string{"--json", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config"}
			wantPath := "action.yml"
			if stdin {
				args = append(args, "--stdin-filename=action.yml", "-")
			} else {
				args = append(args, filepath.Join(dir, "workflow.yml"), filepath.Join(dir, "action.yml"))
				wantPath = workingDirectoryPath(t, filepath.Join(dir, "action.yml"))
			}
			if modern {
				args = append([]string{"check"}, args...)
			}
			got := testRunCommand(manifest, args...)
			var report resultreport.CheckResult
			if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil {
				t.Fatalf("args=%v: invalid JSON: %v; output=%+v", args, err, got)
			}
			if got.Status != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "action" || report.Diagnostics[0].Path != wantPath {
				t.Fatalf("args=%v: expected one metadata error, got %+v, diagnostics=%+v", args, got, report.Diagnostics)
			}
			want := 2
			if stdin {
				want = 1
			}
			if len(report.Documents) != want {
				t.Fatalf("args=%v: want %d documents, got %#v", args, want, report.Documents)
			}
			if _, ok := report.Documents[want-1].(outline.Action); !ok {
				t.Fatalf("args=%v: expected action outline, got %#v", args, report.Documents)
			}
		}
	}
}

func TestCommandDefaultActionDiscovery(t *testing.T) {
	t.Chdir(t.TempDir())
	if out, err := gitclient.Command(t.Context(), "git", ".", "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	const manifest = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ secrets.TOKEN }}\n      shell: bash\n"
	if err := os.WriteFile("action.yml", []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, modern := range []bool{false, true} {
		args := []string{"--json", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config"}
		if modern {
			args = append([]string{"check"}, args...)
		}
		got := testRunCommand("", args...)
		var report resultreport.CheckResult
		if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil {
			t.Fatalf("args=%v: invalid JSON: %v; output=%+v", args, err, got)
		}
		if got.Status != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "action" || report.Diagnostics[0].Path != "action.yml" || len(report.Documents) != 1 {
			t.Fatalf("args=%v: expected one discovered metadata error, got %+v, report=%+v", args, got, report)
		}
	}
}
