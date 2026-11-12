package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestLocalActionRuntimeLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.js"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		using, problem string
		code           actionmeta.ProblemCode
	}{
		{"node24", "", ""},
		{"NODE24", "", ""},
		{"node20", "deprecated", actionmeta.CodeRuntimeDeprecated},
		{"node16", `use "node24" instead`, actionmeta.CodeRuntimeDeprecated},
		{"node12", `use "node24" instead`, actionmeta.CodeRuntimeDeprecated},
		{"node22", "invalid runner name", actionmeta.CodeRuntimeUnsupported},
		{"node999", "invalid runner name", actionmeta.CodeRuntimeUnsupported},
	} {
		t.Run(tc.using, func(t *testing.T) {
			t.Parallel()
			meta := &actionmeta.Metadata{Name: "example", Directory: dir, Runs: actionmeta.Runs{Using: tc.using, Main: "main.js"}}
			rule := validator{}
			rule.checkLocalActionRuns(meta, &workflowlang.Pos{Line: 1, Col: 1})
			errs := rule.errs
			if tc.problem == "" {
				if len(errs) != 0 {
					t.Fatalf("valid runtime rejected: %v", errs)
				}
			} else if len(errs) != 1 || errs[0].Code != string(tc.code) || !strings.Contains(errs[0].Message, tc.problem) {
				t.Fatalf("wanted code %q and message containing %q, got %v", tc.code, tc.problem, errs)
			}
		})
	}
}

func TestDeprecatedRuntimeStillChecksJavaScriptMetadata(t *testing.T) {
	t.Parallel()
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
