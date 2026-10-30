package ruff

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
	pythonprofile "actionlint.kjanat.dev/v2/internal/tools/profile/python"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestRetainsInitializationAndCheckFailures(t *testing.T) {
	initial := errors.New("first configuration failed")
	missing := filepath.Join(t.TempDir(), "missing-directory")
	checker := New(func([]string, string, func([]byte, error) error) { t.Fatal("unexpected execution") }, func() error { return nil }, pythonprofile.TemplateEnd)
	checker.WorkingDirectory(missing)
	first := &config.Config{}
	r := &ruffAnalyzer{checker: checker, forConfig: func(cfg *config.Config) (*Checker, error) {
		if cfg == first {
			return nil, initial
		}
		return checker, nil
	}}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	python := "python"
	_, err := r.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{
		{Text: "print(1)", Key: pos, Shell: &python, Config: first}, {Text: "print(1)", Key: pos, Shell: &python},
	}})
	if !errors.Is(err, initial) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("lost an incomplete-analysis cause: %v", err)
	}
}
