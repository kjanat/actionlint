package application

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/repository"
)

const variableWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env: {VALUE: '${{ vars.%s }}'}\n"

func TestRepositoryHostKnowledgeIsScopedToIdentifiedRoots(t *testing.T) {
	first, second, unknown := t.TempDir(), t.TempDir(), t.TempDir()
	variables, err := repository.NewVariables(repository.Variable{Name: "HOST"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := repository.NewVariables()
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	provider := func(_ context.Context, root string) repository.Snapshot {
		if !filepath.IsAbs(root) {
			t.Errorf("unidentified repository reached host: %q", root)
		}
		calls[root]++
		if root == first {
			return repository.Snapshot{Variables: variables}
		}
		return repository.Snapshot{Variables: empty}
	}
	var sources []workspace.Source
	for i, test := range []struct {
		root, variable string
		identified     bool
	}{
		{first, "HOST", true}, {first, "HOST", true}, {second, "HOST", true}, {unknown, "ANYTHING", false},
	} {
		var project *workspace.Project
		if test.identified {
			project = workspace.Identity(test.root)
		}
		sources = append(sources, workspace.Source{Path: filepath.Join(test.root, fmt.Sprintf("workflow%d.yml", i)), Project: project, Content: fmt.Appendf(nil, variableWorkflow, test.variable)})
	}
	result, err := Analyze(t.Context(), Options{WorkingDir: unknown, Repository: provider}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[first] != 1 || calls[second] != 1 {
		t.Fatalf("host knowledge was not scoped per identified root: %v", calls)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression-reference" || result.Diagnostics[0].Path != sources[2].Path {
		t.Fatalf("host facts leaked between repositories: %+v", result.Diagnostics)
	}
}

func TestEnvironmentNamesAreScopedPerJob(t *testing.T) {
	cfg, err := repoconfig.Parse([]byte(`repository:
  variables: {}
  secrets: []
  environments:
    prod:
      variables: {DEPLOY: null}
      secrets: [ENV_TOKEN]
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, environment, trigger string
		findings                   int
	}{
		{"selected environment", "    environment: prod\n", "push", 0},
		{"different environment", "    environment: test\n", "push", 2},
		{"no environment", "", "push", 2},
		{"dynamic environment", "    environment: '${{ github.ref }}'\n", "push", 0},
		{"selected environment in reusable workflow", "    environment: prod\n", "{workflow_call: {secrets: {}}}", 0},
		{"dynamic environment in reusable workflow", "    environment: '${{ github.ref }}'\n", "{workflow_call: {secrets: {}}}", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			content := "on: " + test.trigger + "\njobs:\n  test:\n" + test.environment + "    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env: {VALUE: '${{ vars.DEPLOY }}', TOKEN: '${{ secrets.ENV_TOKEN }}'}\n"
			result, err := Analyze(t.Context(), Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Config: cfg, Content: []byte(content)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != test.findings {
				t.Fatalf("environment visibility is incorrect: %+v", result.Diagnostics)
			}
			for _, finding := range result.Diagnostics {
				if finding.Rule != "expression-reference" {
					t.Fatalf("unrelated finding: %+v", finding)
				}
			}
		})
	}
}

func TestUnavailableRunnerLabelsDoNotBecomeAnEmptyAllowlist(t *testing.T) {
	for _, test := range []struct {
		config, selector string
		findings         int
	}{
		{"{}", "private-pool", 0}, {"repository: {runners: null}", "private-pool", 0}, {"repository: {runners: []}", "private-pool", 1}, {"repository: {runners: [private-pool]}", "private-pool", 0},
		{"{}", "[ubuntu-latest, windows-latest]", 1},
	} {
		t.Run(test.config+"/"+test.selector, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte(test.config))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			result, err := Analyze(t.Context(), Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Config: cfg, Content: []byte("on: push\njobs:\n  test:\n    runs-on: " + test.selector + "\n    steps:\n      - run: echo ok\n")}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != test.findings {
				t.Fatalf("unknown labels became negative evidence: %+v", result.Diagnostics)
			}
		})
	}
}
