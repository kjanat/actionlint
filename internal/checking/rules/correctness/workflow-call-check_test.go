package correctness

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestWorkflowCallCallbacksUseCapturedDefaultsIndependently(t *testing.T) {
	t.Parallel()
	const source = `on: push
permissions: {}
jobs:
  allowed:
    permissions: write-all
    uses: ./callee.yml
  inherited:
    uses: ./callee.yml
`
	decoded, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	workflow := declaration.Publish(source, decoded, false, "").Bind("caller").Workflow()
	allowed, _ := workflow.Job("allowed")
	inherited, _ := workflow.Job("inherited")
	root := t.TempDir()
	cache := workflowinputs.NewCache(root, nil)
	cache.Store("./callee.yml", &calls.Metadata{JobPermissions: map[string]calls.PermissionScopeLevels{
		"nested": {"contents": calls.PermissionLevelWrite},
	}})
	var messages []string
	check := WorkflowCallCheck(workflow, cache.Resolved(), repository.Snapshot{}, func(format string, args ...any) {
		messages = append(messages, fmt.Sprintf(format, args...))
	})
	if check.Name != "workflow-call" || check.Workflow != nil || check.Step != nil {
		t.Fatalf("unexpected declaration callbacks: %+v", check)
	}
	first := check.Job(inherited)
	if len(first) != 1 || first[0].Rule != "workflow-call" || first[0].Start.Line != 8 || !strings.Contains(first[0].Message, "contents: none") {
		t.Fatalf("captured workflow permissions were not applied: %+v", first)
	}
	if findings := check.Job(allowed); len(findings) != 0 {
		t.Fatalf("job permission override was lost: %+v", findings)
	}
	if repeated := check.Job(inherited); !reflect.DeepEqual(first, repeated) {
		t.Fatal("callback depends on another job's permissions or findings")
	}
	if len(messages) != 3 || !strings.Contains(messages[0], "Validated reusable workflow") {
		t.Fatalf("supplied debug logger was not called: %v", messages)
	}
}
