package engine

import (
	"path/filepath"
	"strings"
	"testing"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestMetadataFirstVisibleInvocation(t *testing.T) {
	for _, metadata := range []struct {
		name, source string
		foreign      bool
	}{
		{"caller", "name: inner\nruns: {using: node24, main: index.js}\n", false},
		{"metadata", "name: inner\ndescription: test\ninputs:\n  token:\n    description: token\n    default: ${{ secrets.TOKEN }}\nruns: {using: node24, main: index.js}\n", true},
	} {
		t.Run(metadata.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, "inner/action.yml", metadata.source)
			writeShellcheckFixture(t, root, "inner/index.js", "")
			for _, tc := range []struct {
				name, ignores string
				first         int
			}{
				{"none", "[]", 0},
				{"unmatched job", "[{rule: action, job: missing}]", 0},
				{"unmatched file", "[{rule: action, file: missing.yml}]", 0},
				{"metadata file", "[{rule: action, file: inner/action.yml}]", 0},
				{"other rule", "[{rule: shellcheck, job: a}]", 0},
				{"first", "[{rule: action, job: c}]", 1},
				{"second", "[{rule: action, job: b}]", 0},
				{"first two", "[{rule: action, job: c}, {rule: action, job: b}]", 2},
				{"all", "[{rule: action, job: a}, {rule: action, job: b}, {rule: action, job: c}]", -1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.name == "metadata file" && metadata.foreign {
						tc.first = -1
					}
					config, err := ParseConfig([]byte("tools: {shellcheck: false, ruff: false}\nignores: " + tc.ignores))
					if err != nil {
						t.Fatal(err)
					}
					var source strings.Builder
					source.WriteString("on: push\njobs:\n")
					for _, job := range []string{"c", "b", "a"} {
						source.WriteString("  ")
						source.WriteString(job)
						source.WriteString(":\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n")
					}
					result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source.String()), Config: config, Project: workspace.Identity(root)}}})
					if err != nil {
						t.Fatal(err)
					}
					want := 1
					if tc.first < 0 {
						want = 0
					}
					if len(result.Diagnostics) != want {
						t.Fatalf("findings: %+v", result.Diagnostics)
					}
					if want == 0 {
						return
					}
					d := result.Diagnostics[0]
					if metadata.foreign {
						if d.Path != filepath.Join("inner", "action.yml") {
							t.Fatalf("lost metadata path: %+v", d)
						}
					} else if d.Start.Line != 6+tc.first*4 {
						t.Fatalf("wrong first visible caller: %+v", d)
					}
				})
			}
		})
	}
}

func TestMetadataValidationDebugLog(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeShellcheckFixture(t, root, "inner/index.js", "")
	var cacheLog, ruleLog strings.Builder
	rule := NewRuleAction(actionlocal.NewCache(projectRoot(workspace.Identity(root)), &cacheLog))
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
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\nruns: {using: node24, main: index.js}\n")
	writeShellcheckFixture(t, root, "inner/index.js", "")
	cache := actionlocal.NewCache(projectRoot(workspace.Identity(root)), nil)
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
