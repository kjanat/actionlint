package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/render"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
)

type customRuleForTest struct {
	workflowvisit.Base
	count int
}

func (r *customRuleForTest) VisitStep(n *workflowlang.Step) error {
	r.count++
	if r.count > 1 {
		r.Errorf(n.Pos, "only single step is allowed but got %d steps", r.count)
	}
	return nil
}

func TestTraversalAddsCustomRule(t *testing.T) {
	request := Request{
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			r := &customRuleForTest{
				Base: workflowvisit.NewBase("this-is-test", ""),
			}
			return append(rules, r)
		},
	}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
`
		request.Sources = []SourceUnit{{Path: "test.yaml", Content: []byte(w), Config: &repoconfig.Config{}}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 0 {
			t.Fatal("wanted no error but have", errs)
		}
	}

	{
		w := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo
      - run: echo
`
		request.Sources = []SourceUnit{{Path: "test.yaml", Content: []byte(w), Config: &repoconfig.Config{}}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 {
			t.Fatal("wanted 1 error but have", errs)
		}

		var b strings.Builder
		render.FindingOf(errs[0].Diagnostic, nil).PrettyPrint(&b)
		have := b.String()
		want := "test.yaml:7:9: only single step is allowed but got 2 steps [this-is-test]\n"
		if have != want {
			t.Fatalf("wanted error message %q but have %q", want, have)
		}
	}
}

func TestTraversalRemovesBuiltinRule(t *testing.T) {
	request := Request{
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			for i, r := range rules {
				if r.Name() == "runner-label" {
					rules = append(rules[:i], rules[i+1:]...)
					break
				}
			}
			return rules
		},
	}

	f := filepath.Join("testdata", "err", "invalid_runner_labels.yaml")
	request.Sources = readFixtureSources(t, []string{f}, nil, &repoconfig.Config{})
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatal("no error was expected because runner-label rule was removed but got:", errs)
	}
}

func BenchmarkLintWorkflowContent(b *testing.B) {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	proj := workspace.Identity(dir)

	// Measure performance of traversing with checks except for external process rules (shellcheck, pyflakes)
	// Reading file content is not included in benchmark measurement.

	for _, name := range []string{"minimal", "small", "large"} {
		var f string
		switch name {
		case "minimal":
			f = filepath.Join(dir, "testdata", "bench", "minimal.yaml")
		case "small":
			f = filepath.Join(dir, "testdata", "bench", "small.yaml")
		case "large":
			f = filepath.Join(dir, "testdata", "bench", "many_scripts.yaml")
		}
		content, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(name, func(b *testing.B) {
			for range b.N {
				result, err := runTestAnalysis(b.Context(), Request{WorkingDir: dir, Sources: []SourceUnit{{Path: f, Content: content, Project: proj, Config: &repoconfig.Config{}}}})
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Diagnostics) > 0 {
					b.Fatal("some error occurred:", result.Diagnostics)
				}
			}
		})
	}
}
