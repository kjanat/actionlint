package ruff

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
)

func TestActionInputDoesNotLoadRuffConfiguration(t *testing.T) {
	runner := &ruffAnalyzer{forConfig: func(*config.Config) (*Checker, error) {
		t.Error("action input requested a Python configuration")
		return nil, nil
	}}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{
		Input: &analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}, Text: "missing();",
	}}})
	if err != nil || len(findings) != 0 {
		t.Fatalf("Ruff analyzed an action input: findings=%+v err=%v", findings, err)
	}
}
