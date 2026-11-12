package checking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestWorkflowMetadataIsPreparedBeforeRuleConstruction(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reads := map[string]int{}
	constructed := false
	_, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources:    []sourceUnit{{Path: "caller.yml", Project: workspace.Identity(root), Content: []byte("on: push\npermissions: {}\njobs: {call: {uses: ./outer.yml}}\n")}},
		ReadFile: func(path string) ([]byte, error) {
			switch filepath.Base(path) {
			case "outer.yml":
				reads["outer"]++
				return []byte("on: workflow_call\njobs: {nested: {uses: ./inner.yml}}\n"), nil
			case "inner.yml":
				reads["inner"]++
				return []byte("on: workflow_call\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"), nil
			default:
				return os.ReadFile(path)
			}
		},
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			constructed = true
			if reads["outer"] != 1 || reads["inner"] != 1 {
				t.Errorf("checks constructed before dependency preparation: %v", reads)
			}
			return rules
		},
	})
	if err != nil || !constructed {
		t.Fatalf("workflow preparation failed: constructed=%t error=%v", constructed, err)
	}
	if reads["outer"] != 1 || reads["inner"] != 1 {
		t.Fatalf("dependency metadata was reread during checks: %v", reads)
	}
}

func TestWorkflowPreparationObservesRequestCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	constructed := false
	_, err := runTestAnalysis(ctx, analysisRequest{
		WorkingDir: root,
		Sources:    []sourceUnit{{Path: "caller.yml", Project: workspace.Identity(root), Content: []byte("on: push\njobs: {call: {uses: ./callee.yml}}\n")}},
		ReadFile: func(path string) ([]byte, error) {
			if filepath.Base(path) == "callee.yml" {
				cancel()
				return []byte("on: workflow_call\njobs: {test: {runs-on: ubuntu-latest, steps: []}}\n"), nil
			}
			return os.ReadFile(path)
		},
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			constructed = true
			return rules
		},
	})
	if !errors.Is(err, context.Canceled) || constructed {
		t.Fatalf("request cancellation did not stop preparation: constructed=%t error=%v", constructed, err)
	}
}

func TestWorkflowPreparationReportsInvalidDependencies(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid YAML", true: "missing file"}[missing], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			result, err := runTestAnalysis(t.Context(), analysisRequest{
				WorkingDir: root,
				Sources:    []sourceUnit{{Path: "caller.yml", Project: workspace.Identity(root), Content: []byte("on: push\njobs: {call: {uses: ./callee.yml}}\n")}},
				ReadFile: func(path string) ([]byte, error) {
					if filepath.Base(path) == "callee.yml" {
						if missing {
							return nil, os.ErrNotExist
						}
						return []byte("on: [broken\n"), nil
					}
					return os.ReadFile(path)
				},
			})
			if err != nil || result == nil {
				t.Fatalf("invalid input became an operation failure: %v", err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				found = found || diagnostic.Rule == "workflow-call"
			}
			if !found {
				t.Fatal("invalid dependency lost its caller diagnostic")
			}
		})
	}
}

func TestWorkflowPreparationPreservesOperationalFailures(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, os.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			constructed := false
			result, err := runTestAnalysis(t.Context(), analysisRequest{
				WorkingDir: root,
				Sources:    []sourceUnit{{Path: "caller.yml", Project: workspace.Identity(root), Content: []byte("on: push\njobs: {call: {uses: ./callee.yml}}\n")}},
				ReadFile: func(path string) ([]byte, error) {
					if filepath.Base(path) == "callee.yml" {
						return nil, &os.PathError{Op: "read", Path: path, Err: cause}
					}
					return os.ReadFile(path)
				},
				OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
					constructed = true
					return rules
				},
			})
			if !errors.Is(err, cause) || constructed || result == nil {
				t.Fatalf("operation failure reached rule callbacks: constructed=%t result-present=%t error=%v", constructed, result != nil, err)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "workflow-call" {
					t.Fatalf("operation failure became a lint finding: %+v", diagnostic)
				}
			}
		})
	}
}
