package selection_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/selection"
	fixture "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/input/files"
	nameinput "actionlint.kjanat.dev/v2/internal/input/files/names"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func workflowNameInputs(t *testing.T, level string, workflowRun bool, load func(string) (string, bool, error)) ruleexec.Inputs {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "consumer.yml")
	trigger := "push"
	if workflowRun {
		trigger = "{workflow_run: {workflows: [Missing], types: [completed]}}"
	}
	source := "on: " + trigger + "\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
	_, _, snapshots := fixture.NewCapturedSnapshots(t, root)
	snapshot, err := snapshots.Document(path, []byte(source), parsing.Workflow([]byte(source)))
	if err != nil {
		t.Fatal(err)
	}
	rules := checking.ConfigurationRules()
	config, err := rules.Parse([]byte("lint: {rules: {correctness: {workflow-run-names: " + level + "}}}"))
	if err != nil {
		t.Fatal(err)
	}
	return ruleexec.Inputs{
		Rules: rules, Config: config, Context: t.Context(), SourceSnapshot: snapshot,
		Path: path, WorkingDir: root, ProjectRoot: root,
		WorkflowNames: nameinput.NewIndex([]string{path}, files.New(t.Context(), nil, nil), load),
	}
}

func TestWorkflowNamePreparationSeparatesLoadingFromCallbacks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, level string
		workflowRun bool
		loads       int
	}{
		{"enabled", "on", true, 1},
		{"disabled", "off", true, 0},
		{"no producer filters", "on", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loads := 0
			inputs := workflowNameInputs(t, tc.level, tc.workflowRun, func(string) (string, bool, error) {
				loads++
				return "Build", true, nil
			})
			found := false
			for _, registration := range ruleexec.Registrations(inputs.Rules) {
				if registration.Name != "workflow-run-names" {
					continue
				}
				found = true
				if registration.Prepare == nil || !registration.Enabled(inputs) {
					t.Fatal("workflow-name preparation is unavailable")
				}
				outcome, err := registration.Prepare(inputs)
				if err != nil {
					t.Fatal(err)
				}
				prepared, ready := outcome.(ruleexec.PreparedInputs)
				if !ready {
					t.Fatalf("preparation did not supply inputs: %T", outcome)
				}
				wantLoads := tc.loads
				if loads != wantLoads || inputs.WorkflowNameInventory.Complete() {
					t.Fatalf("loads=%d, want %d; preparation mutated its input", loads, wantLoads)
				}
				workflow, _ := inputs.SourceSnapshot.WorkflowView()
				rule := registration.Build(prepared.Inputs)
				if err := rule.VisitWorkflowPre(workflow); err != nil {
					t.Fatal(err)
				}
				if loads != wantLoads || len(rule.Errs()) != wantLoads {
					t.Fatalf("callback loaded files or lost findings: loads=%d findings=%v", loads, rule.Errs())
				}
			}
			if !found {
				t.Fatal("workflow-run-names is not registered")
			}
		})
	}
}

func TestPreparationPreservesCancellationAsOperationFailure(t *testing.T) {
	t.Parallel()
	loads := 0
	inputs := workflowNameInputs(t, "on", true, func(string) (string, bool, error) {
		loads++
		return "", false, context.Canceled
	})
	selected, err := selection.CreateRules(inputs.Rules, inputs, false)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "prepare workflow-run-names inputs") || loads != 1 {
		t.Fatalf("preparation failure lost its cause or operation: error=%v loads=%d", err, loads)
	}
	earlier := map[string]bool{}
	for _, registration := range ruleexec.Registrations(inputs.Rules) {
		if registration.Name == "workflow-run-names" {
			break
		}
		earlier[registration.Name] = true
	}
	for _, rule := range selected.Rules {
		if !earlier[rule.Name()] {
			t.Fatalf("constructed %s after failed input preparation", rule.Name())
		}
	}
	for _, script := range selected.ScriptInfo {
		if !earlier[script.Name] {
			t.Fatalf("selected %s after failed input preparation", script.Name)
		}
	}
}

func TestWorkflowNamePreparationReportsUnavailableKnowledge(t *testing.T) {
	t.Parallel()
	inputs := workflowNameInputs(t, "on", true, func(string) (string, bool, error) {
		return "", false, nil
	})
	selected, err := selection.CreateRules(inputs.Rules, inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Unavailable) != 1 || selected.Unavailable[0].Name != "workflow-run-names" || selected.Unavailable[0].Reason == "" {
		t.Fatalf("missing unavailable-check outcome: %+v", selected.Unavailable)
	}
	for _, check := range selected.Rules {
		if check.Name() == "workflow-run-names" {
			t.Fatal("constructed a check without its required knowledge")
		}
	}
}
