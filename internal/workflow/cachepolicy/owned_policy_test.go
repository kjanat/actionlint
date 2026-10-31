package cachepolicy

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func TestTrustReadsPublishedDeclarations(t *testing.T) {
	mode := &workflowlang.CacheMode{Kind: workflowlang.CacheModeWrite, Pos: &workflowlang.Pos{Line: 2, Col: 13}}
	hook := &workflowlang.String{Value: "pull_request_target"}
	job := &workflowlang.Job{}
	model := &workflowlang.Workflow{
		On:        []workflowlang.Event{&workflowlang.WebhookEvent{Hook: hook}},
		CacheMode: mode,
		Jobs:      map[string]*workflowlang.Job{"build": job},
	}
	view := policyWorkflowView(model)
	trust := NewTrust(view)
	mode.Kind = workflowlang.CacheModeNone
	hook.Value = "push"
	job.If = &workflowlang.String{Value: "false"}
	ownedJob, exists := view.JobsAt("build")
	if !exists {
		t.Fatal("published job missing")
	}
	findings := trust.CheckWrites(ownedJob)
	if len(findings) != 1 || findings[0].Start.Line != 2 || !strings.Contains(findings[0].Message, `"pull_request_target"`) {
		t.Fatalf("published write grant changed with decoder model: %v", findings)
	}
	if findings := trust.CheckWrites(ownedJob); len(findings) != 0 {
		t.Fatalf("shared declaration reported twice: %v", findings)
	}
}

type fixedMetadata struct{ metadata *calls.Metadata }

func (m fixedMetadata) FindMetadataForCall(string) (*calls.Metadata, error) {
	return m.metadata, nil
}

func TestReusableMetadataGrantBoundary(t *testing.T) {
	const source = "on: push\ncache-mode: read\njobs:\n  caller:\n    uses: ./.github/workflows/child.yml\n"
	parsed, findings := workflowparser.ParseDocument([]byte(source))
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	view := policyWorkflowView(parsed)
	job, exists := view.JobsAt("caller")
	if !exists {
		t.Fatal("caller missing")
	}
	for _, tc := range []struct {
		name               string
		mode               calls.CacheModeDeclaration
		ceiling, operation int
	}{
		{"inherited", calls.CacheModeDeclaration{}, 0, 1},
		{"invalid", calls.DeclaredCacheMode(workflowlang.CacheModeInvalid), 0, 0},
		{"explicit read", calls.DeclaredCacheMode(workflowlang.CacheModeRead), 0, 0},
		{"explicit write exceeds caller", calls.DeclaredCacheMode(workflowlang.CacheModeWrite), 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := &calls.Metadata{JobCacheAccess: map[string]calls.CacheAccess{
				"nested": {Mode: tc.mode, Operations: []string{"actions/cache/save"}},
			}}
			ceiling := CheckCeiling(job.WorkflowCall(), view.CacheMode(), metadata, fixedMetadata{metadata}, nil)
			operations := CheckCallOperations(job.WorkflowCall(), view.CacheMode(), fixedMetadata{metadata})
			if len(ceiling) != tc.ceiling || len(operations) != tc.operation {
				t.Fatalf("ceiling=%v operations=%v; want counts %d/%d", ceiling, operations, tc.ceiling, tc.operation)
			}
			for _, findings := range [][]diagnostic.Diagnostic{ceiling, operations} {
				for _, finding := range findings {
					if finding.Start.Line != 5 || finding.Start.Column != 11 || !strings.Contains(finding.Message, `job "nested"`) {
						t.Errorf("finding lost caller position or metadata job: %v", finding)
					}
				}
			}
		})
	}
}
