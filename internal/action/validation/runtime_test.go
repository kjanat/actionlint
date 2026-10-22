package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

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
			meta := &actionmeta.Metadata{Name: "example", Directory: dir, Runs: actionmeta.Runs{Using: tc.using, Main: "main.js"}}
			rule := validator{}
			rule.checkLocalActionRuns(meta, &workflowlang.Pos{Line: 1, Col: 1})
			errs := rule.errs
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
	rule := validator{}
	meta := &actionmeta.Metadata{Name: "example", Runs: actionmeta.Runs{Using: "node20"}}
	rule.checkLocalActionRuns(meta, &workflowlang.Pos{Line: 1, Col: 1})
	var deprecated, missingMain bool
	for _, err := range rule.errs {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingMain = missingMain || strings.Contains(err.Message, `"main" is required`)
	}
	if !deprecated || !missingMain {
		t.Fatalf("lost independent metadata validation: %v", rule.errs)
	}
}
