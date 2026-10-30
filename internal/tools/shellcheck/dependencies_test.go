package shellcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/fsread"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func TestShellcheckSourceInventoryWithoutDiagnostics(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	pool := analyzerhost.NewProcessPool(t.Context(), 1)
	want := []string{filepath.Join(root, "clean.sh"), filepath.Join(root, "nested.sh"), filepath.Join(outside, "external.sh")}
	data, err := json.Marshal(shellcheckResult{Sources: []string{"clean.sh", "nested.sh", want[2]}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := pool.EmbeddedCommand("shellcheck", func([]string, string) ([]byte, error) { return data, nil })
	run := newShellcheckRunForTest(cmd)
	var inputs []string
	run.paths.reader = fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })
	if err := run.runShellcheck("echo ok", workflowdecl.ScriptSourceView{}, shellprofile.Shell{Name: "bash"}, runDirectory{directoryKnown, root}, &workflowlang.Pos{Line: 1, Col: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(run.errs) != 0 {
		t.Fatalf("findings=%v inputs=%v; want %v", run.errs, inputs, want)
	}
	for _, path := range want {
		if !slices.Contains(inputs, path) {
			t.Fatalf("missing source %s: %v", path, inputs)
		}
	}
}

func TestSourceInventoryResolvesSymlinksBeforeParents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	file := writeShellcheckFixture(t, root, "real/helper.sh", "VALUE=correct\n")
	writeShellcheckFixture(t, root, "helper.sh", "VALUE=wrong\n")
	if err := os.Mkdir(filepath.Join(root, "real", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var inputs []string
	run := &shellcheckRun{paths: runPaths{reader: fsread.New(t.Context(), nil, func(path string) { inputs = append(inputs, path) })}}
	if err := run.recordSources([]string{"link/../helper.sh"}, root); err != nil {
		t.Fatal(err)
	}
	recorded := func(name string) bool {
		want, err := os.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range inputs {
			got, err := os.Lstat(input)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(want, got) {
				return true
			}
		}
		return false
	}
	if !recorded(file) || !recorded(filepath.Join(root, "link")) || recorded(filepath.Join(root, "helper.sh")) {
		t.Fatalf("wrong source identity: %v", inputs)
	}
	finding := run.sourcedDiagnostic(shellcheckError{File: "link/../helper.sh", Line: 1, Column: 1}, root, map[string]reportedSource{})
	if string(finding.source) != "VALUE=correct\n" {
		t.Fatalf("reported parent traversal reread the lexically cleaned wrong file: %q", finding.source)
	}
}
