package builtin

import (
	"path/filepath"
	"strings"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestMetadataValidationDebugLog(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeFixture(t, root, "inner/index.js", "")
	var cacheLog, ruleLog strings.Builder
	rule := NewRuleAction(actionlocal.NewCache(root, &cacheLog))
	rule.EnableDebug(&ruleLog)
	for i := 1; i <= 2; i++ {
		rule.checkLocalAction("./inner", "./inner", &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "./inner", Pos: &workflowlang.Pos{Line: i, Col: 1}}})
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
		rule.checkLocalAction("./inner", "./inner", action)
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
