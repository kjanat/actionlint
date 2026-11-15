package checking

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/source"
	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCombinedProjectsPreserveIncompleteChecks(t *testing.T) {
	t.Parallel()
	checks := []report.IncompleteCheck{
		{Source: source.Reference{Document: "first/ci.yml", Revision: "first"}, Rule: "shellcheck", Reason: "tool unavailable"},
		{Source: source.Reference{Document: "second/ci.yml", Revision: "second"}, Reason: "policy evaluation failed"},
	}
	var combined report.Analysis
	for _, check := range checks {
		var err error
		combined, err = report.Merge(combined, report.Analysis{IncompleteChecks: []report.IncompleteCheck{check}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(combined.IncompleteChecks, checks) {
		t.Fatalf("combined repository checks lost completion evidence:\n got: %+v\nwant: %+v", combined.IncompleteChecks, checks)
	}
}

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
			if output, err := gitclient.Command(t.Context(), "git", root, args...).CombinedOutput(); err != nil {
				t.Fatalf("prepare repository: %v: %s", err, output)
			}
		}
		paths = append(paths, consumer)
	}
	result, err := CheckProjects(t.Context(), Options{SkipProjectConfig: true}, inputmodel.Request{Kind: inputmodel.Files, Paths: paths}, plugininstall.Selection{Disabled: true, DisableTools: true})
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
		if !seen[filepath.Clean(path)] {
			t.Errorf("missing incomplete workflow-name check for %s: %+v", path, result.IncompleteChecks)
		}
	}
	if result.FileCount != 2 || len(result.Sources) != len(result.Diagnostics) {
		t.Fatalf("invalid merged report: files=%d findings=%d sources=%d", result.FileCount, len(result.Diagnostics), len(result.Sources))
	}
	inputs := map[string]bool{}
	for _, path := range result.Inputs {
		if inputs[path] {
			t.Fatalf("duplicate input %q", path)
		}
		inputs[path] = true
	}
}
