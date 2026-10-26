package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestActionValidationPropagatesMetadataInterruption(t *testing.T) {
	for _, spec := range []string{"./inner", "$/inner"} {
		for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(spec+"/"+failure.Error(), func(t *testing.T) {
				cache := actionlocal.NewCache(t.TempDir(), nil)
				cache.Configure(actionlocal.Options{CustomReader: true, Reader: fsread.New(t.Context(), func(string) ([]byte, error) {
					return nil, failure
				}, nil)})
				rule := NewRuleAction(cache)
				err := rule.VisitStep(&workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: &workflowlang.Pos{Line: 1, Col: 1}}}})
				if !errors.Is(err, failure) || len(rule.Errs()) != 0 {
					t.Fatalf("interruption became a lint finding: error=%v findings=%+v", err, rule.Errs())
				}
			})
		}
	}
}

func TestMetadataValidationDebugLog(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeFixture(t, root, "inner/index.js", "")
	var cacheLog, ruleLog strings.Builder
	rule := NewRuleAction(actionlocal.NewCache(root, &cacheLog))
	rule.EnableDebug(&ruleLog)
	for i := 1; i <= 2; i++ {
		if err := rule.checkLocalAction("./inner", "./inner", &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./inner", Pos: &workflowlang.Pos{Line: i, Col: 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	if want := "[LocalActionsCache] New metadata parsed from action " + filepath.Join(root, "inner") + ": node24 action \"inner\"\n"; !strings.Contains(cacheLog.String(), want) {
		t.Fatalf("cache log lacks %q: %q", want, cacheLog.String())
	}
	if want := "[action] Checking metadata of node24 action \"inner\" at \"./inner\"\n"; ruleLog.String() != want {
		t.Fatalf("rule log = %q, want %q", ruleLog.String(), want)
	}
}

func TestMetadataValidationTemplatesDetached(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeFixture(t, root, "inner/index.js", "")
	cache := actionlocal.NewCache(root, nil)
	rule := NewRuleAction(cache)
	for i := 1; i <= 3; i++ {
		action := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./inner", Pos: &workflowlang.Pos{Line: i, Col: 1}}}
		if err := rule.checkLocalAction("./inner", "./inner", action); err != nil {
			t.Fatal(err)
		}
	}
	if len(rule.validation.templates) != 1 || len(rule.Errs()) != 3 {
		t.Fatalf("templates=%d findings=%d", len(rule.validation.templates), len(rule.Errs()))
	}
	for i, finding := range rule.Errs() {
		if finding.Start.Line != i+1 || !strings.Contains(finding.Message, "description is required") {
			t.Fatalf("%+v", finding)
		}
	}
	rule.Errs()[0].Message = "mutated caller"
	if rule.validation.templates[filepath.Join(root, "inner", "action.yml")][0].Message == "mutated caller" {
		t.Fatal("caller mutated cached template")
	}
}
