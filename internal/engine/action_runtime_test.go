package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestNode20HostedMigration(t *testing.T) {
	for _, using := range []string{"node20", "NODE20"} {
		message := actionRuntimeProblem(using)
		for _, want := range []string{"GitHub.com", "Node 24", "no longer supports the Node 20 opt-out"} {
			if !strings.Contains(message, want) {
				t.Errorf("runtime %s: wanted %q in %q", using, want, message)
			}
		}
		if strings.Contains(message, "scheduled") || strings.Contains(message, "no longer bundled") {
			t.Errorf("runtime %s: conflated hosted migration with bundle inventory: %s", using, message)
		}
	}
}

func TestLocalActionRuntimeLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.js"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ using, problem string }{
		{"node24", ""},
		{"NODE24", ""},
		{"node20", "deprecated"},
		{"node16", "no longer bundled"},
		{"node12", "no longer bundled"},
		{"node22", "invalid runner name"},
		{"node999", "invalid runner name"},
	} {
		t.Run(tc.using, func(t *testing.T) {
			meta := &ActionMetadata{Name: "example", dir: dir, Runs: ActionMetadataRuns{Using: tc.using, Main: "main.js"}}
			rule := NewRuleAction(nil)
			rule.checkLocalActionRuns(meta, &workflowlang.Pos{Line: 1, Col: 1})
			errs := rule.Errs()
			if tc.problem == "" {
				if len(errs) != 0 {
					t.Fatalf("valid runtime rejected: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.problem) {
				t.Fatalf("wanted %q, got %v", tc.problem, errs)
			}
		})
	}
}

func TestDeprecatedRuntimeStillChecksJavaScriptMetadata(t *testing.T) {
	rule := NewRuleAction(nil)
	meta := &ActionMetadata{Name: "example", Runs: ActionMetadataRuns{Using: "node20"}}
	rule.checkLocalActionRuns(meta, &workflowlang.Pos{Line: 1, Col: 1})
	var deprecated, missingMain bool
	for _, err := range rule.Errs() {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingMain = missingMain || strings.Contains(err.Message, `"main" is required`)
	}
	if !deprecated || !missingMain {
		t.Fatalf("lost independent metadata validation: %v", rule.Errs())
	}
}

func TestDeprecatedPopularActionKeepsInputAndOutputMetadata(t *testing.T) {
	const spec = "actions/github-script@v7"
	meta := PopularActions[spec]
	if meta == nil || meta.Runs.Using != "node20" || meta.Outputs["result"] == nil {
		t.Fatal("deprecated action lost its runtime or output metadata")
	}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: pos}}
	rule := NewRuleAction(nil)
	rule.checkRepoAction(spec, exec)
	var deprecated, missingScript bool
	for _, err := range rule.Errs() {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingScript = missingScript || strings.Contains(err.Message, `missing input "script"`)
	}
	if !deprecated || !missingScript {
		t.Fatalf("deprecation disabled input validation: %v", rule.Errs())
	}
}
