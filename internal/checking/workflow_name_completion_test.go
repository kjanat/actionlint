package checking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestIncompleteWorkflowNamesDoNotProveUnusedSuppression(t *testing.T) {
	for _, tc := range []struct {
		name, producer, selector string
		wantUnused               bool
	}{
		{"incomplete", "name: [broken\n", "workflow-run-names", false},
		{"complete", "name: Missing\non: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", "workflow-run-names", true},
		{"unrelated selector", "name: [broken\n", "workflow-run-names permissions", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".github", "workflows")
			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "producer.yml"), []byte(tc.producer), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", ".github"}} {
				if output, err := gitclient.Command(t.Context(), "git", root, args...).CombinedOutput(); err != nil {
					t.Fatalf("prepare repository index: %v: %s", err, output)
				}
			}
			const source = `on:
  workflow_run:
    workflows: [Missing] # ignore[workflow-run-names]
    types: [completed]
permissions: {}
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
			result, err := Analyze(t.Context(), Options{
				WorkingDir: root, SkipProjectConfig: true,
				Tools: registry.Registrations(registry.Selections{
					"shellcheck": {Process: &processconfig.Options{Executable: new("")}},
					"ruff":       {Process: &processconfig.Options{Executable: new("")}},
				}),
			}, []workspace.Source{{Path: filepath.Join(directory, "consumer.yml"), Content: []byte(strings.Replace(source, "ignore[workflow-run-names]", "ignore["+tc.selector+"]", 1)), Project: workspace.Identity(root)}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "complete" {
				if len(result.IncompleteChecks) != 0 {
					t.Fatalf("complete inventory reported incomplete checks: %+v", result.IncompleteChecks)
				}
			} else if len(result.IncompleteChecks) != 1 || result.IncompleteChecks[0].Rule != "workflow-run-names" || result.IncompleteChecks[0].Source.Document == "" || result.IncompleteChecks[0].Source.Revision == "" || result.IncompleteChecks[0].Reason == "" {
				t.Fatalf("result lost the incomplete check and its captured source: %+v", result.IncompleteChecks)
			}
			if tc.name != "complete" {
				check := result.IncompleteChecks[0]
				if check.Invocation.ID == "" || check.Invocation.Entry.Document != check.Source.Document || check.Invocation.Entry.Revision != check.Source.Revision {
					t.Fatalf("preparation failure lost the entry invocation: %+v", check)
				}
			}
			want := 0
			if tc.wantUnused {
				want = 1
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("got %d findings, want %d: %+v", len(result.Diagnostics), want, result.Diagnostics)
			}
			if want != 0 && (result.Diagnostics[0].Rule != "unused-suppression" || len(result.Diagnostics[0].Fixes) == 0) {
				t.Fatalf("proved-unused selector has no removal fix: %+v", result.Diagnostics)
			}
			if tc.name == "unrelated selector" {
				for _, fix := range result.Diagnostics[0].Fixes {
					for _, edit := range fix.Edits {
						if !strings.Contains(edit.Replacement, "workflow-run-names") || strings.Contains(edit.Replacement, "permissions") {
							t.Fatalf("fix must remove only the proved-unused selector: %+v", edit)
						}
					}
				}
			}
		})
	}
}
