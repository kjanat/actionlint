package checking

import (
	"path/filepath"
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
)

func TestWorkflowMetadataPreparationDoesNotRequireLintRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, trigger string
		metadata      bool
	}{
		{"reusable", "{workflow_call: {outputs: {artifact: {value: '${{ jobs.build.outputs.artifact }}'}}}}", true},
		{"ordinary", "push", false},
		{"invalid reusable metadata", "{workflow_call: {inputs: invalid}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			cache := newCapturedWorkflowCache(t, root, root, nil)
			content := []byte("on: " + tc.trigger + "\njobs:\n  build:\n    runs-on: ubuntu-latest\n    outputs: {artifact: value}\n    steps: [{run: echo ok}]\n")
			_, err := checkWithCaches(t, t.Context(), analysisRequest{
				WorkingDir:     root,
				OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule { return nil },
			}, sourceUnit{Path: filepath.Join(root, "workflow.yml"), Content: content}, nil, nil, cache)
			if err != nil {
				t.Fatal(err)
			}
			metadata, failure, cached := cache.Lookup("./workflow.yml")
			if !cached || (metadata != nil) != tc.metadata || (failure == nil) != tc.metadata {
				t.Fatalf("prepared metadata=%+v failure=%v cached=%t", metadata, failure, cached)
			}
			if tc.metadata {
				if _, declared := metadata.Outputs["artifact"]; !declared {
					t.Fatalf("captured output declaration was lost: %+v", metadata)
				}
			}
		})
	}
}
