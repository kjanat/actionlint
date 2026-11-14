package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"

	"actionlint.kjanat.dev/v2/internal/results/outline"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCommandSymlinkSourcePaths(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("real", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", "link"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files := map[string]string{
		"workflow.yml": commandBadWorkflow,
		"action.yml":   "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ secrets.TOKEN }}\n      shell: bash\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join("real", name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{filepath.Join("link", "action.yml"), filepath.Join("link", "workflow.yml")}
	got := testRunCommand("", "check", "--json", "--summary", "--no-config", want[0], want[1])
	var result resultreport.CheckResult
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status != 1 {
		t.Fatalf("check: %+v, %v", got, err)
	}
	var paths []string
	for _, diagnostic := range result.Diagnostics {
		paths = append(paths, diagnostic.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Fatalf("diagnostic paths = %q, want %q", paths, want)
	}
	paths = nil
	for _, document := range result.Documents {
		paths = append(paths, document.DocumentPath())
	}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Fatalf("document paths = %q, want %q", paths, want)
	}
	var summary struct {
		Summary resultreport.CheckSummary `json:"summary"`
	}
	if err := json.Unmarshal([]byte(got.Stderr), &summary); err != nil {
		t.Fatal(err)
	}
	paths = nil
	for _, file := range summary.Summary.ByFile {
		paths = append(paths, file.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Fatalf("summary paths = %q, want %q", paths, want)
	}
}

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
