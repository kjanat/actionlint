package assembly

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestCheckProjectsPreservesIncompleteChecksAcrossRepositories(t *testing.T) {
	t.Parallel()
	var paths []string
	for range 2 {
		root := t.TempDir()
		dir := filepath.Join(root, ".github", "workflows")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "producer.yml"), []byte("name: [broken\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		consumer := filepath.Join(dir, "consumer.yml")
		workflow := "on: {workflow_run: {workflows: [Missing], types: [completed]}}\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
		if err := os.WriteFile(consumer, []byte(workflow), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", ".github"}} {
			if output, err := gitcommand.Command(t.Context(), "git", root, args...).CombinedOutput(); err != nil {
				t.Fatalf("prepare repository: %v: %s", err, output)
			}
		}
		paths = append(paths, consumer)
	}
	result, err := CheckProjects(t.Context(), checking.Options{SkipProjectConfig: true}, checkrequest.Request{Kind: checkrequest.Files, Paths: paths}, pluginselection.PluginSelection{Disabled: true, DisableTools: true})
	if err != nil || result == nil {
		t.Fatalf("check projects: result=%+v error=%v", result, err)
	}
	seen := map[string]bool{}
	for _, check := range result.IncompleteChecks {
		if check.Rule == "workflow-run-names" && check.Source.Document != "" && check.Source.Revision != "" && check.Reason != "" {
			seen[filepath.Clean(check.Source.Document)] = true
		}
	}
	for _, path := range paths {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if !seen[filepath.Clean(canonical)] {
			t.Errorf("missing incomplete workflow-name check for %s: %+v", path, result.IncompleteChecks)
		}
	}
	if result.FileCount != 2 {
		t.Fatalf("invalid merged report: files=%d findings=%d", result.FileCount, len(result.Diagnostics))
	}
	inputs := map[string]bool{}
	for _, path := range result.Inputs {
		if inputs[path] {
			t.Fatalf("duplicate input %q", path)
		}
		inputs[path] = true
	}
}
