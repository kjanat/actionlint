package actions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestAgenticWorkflowCompiledLockDiscoveryAndAnalysis(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("internal", "development", "tests", "integration", "actions", "testdata", "agentic", "smoke-claude-auto.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(workflowDir, "smoke-claude-auto.lock.yml")
	if err := os.WriteFile(lockPath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	// The source Markdown is deliberately not supplied as Actions YAML.
	if err := os.WriteFile(filepath.Join(workflowDir, "smoke-claude-auto.md"), []byte("---\nengine: claude\n---\nReview issues.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := session.Directory(workflowDir, workspace.Identity(root))
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(".github", "workflows", "smoke-claude-auto.lock.yml")
	if len(selection.Sources) != 1 || selection.Sources[0].Path != wantPath {
		t.Fatalf("workflow discovery selected %q; want only compiled lock file %q", workflowSourcePaths(selection.Sources), wantPath)
	}
	findings, err := testutil.Diagnostics(checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, selection.Sources))
	if err != nil || len(findings) != 0 {
		t.Fatalf("compiled workflow diagnostics = %v, %v; want none", findings, err)
	}

	mutated := strings.Replace(string(fixture), "needs.admission.outputs.result", "needs.admission.outputs..result", 1)
	if mutated == string(fixture) {
		t.Fatal("mutation target not found in fixture")
	}
	selection.Sources[0].Content = []byte(mutated)
	findings, err = testutil.Diagnostics(checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, selection.Sources))
	if err != nil || len(findings) != 1 || findings[0].Rule != "expression-syntax" {
		t.Fatalf("malformed generated expression diagnostics = %v, %v; want one expression-syntax diagnostic", findings, err)
	}
}

func workflowSourcePaths(sources []workspace.Source) []string {
	result := make([]string, len(sources))
	for i, source := range sources {
		result[i] = source.Path
	}
	return result
}
