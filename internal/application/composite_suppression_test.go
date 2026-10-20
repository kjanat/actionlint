package application

import (
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/rule"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestLocalActionSuppressionSourcesStayWorkflowScoped(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/index.js", "console.log('ok');\n")
	writeShellcheckFixture(t, root, "local/action.yml", `name: local # actionlint:ignore action --
description: test
runs:
  using: node24
  main: index.js
`)
	used := writeShellcheckFixture(t, root, ".github/workflows/used.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local
`)
	unused := writeShellcheckFixture(t, root, ".github/workflows/unused.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`)
	project := workspace.Identity(root)
	actions := actionlocal.NewCache(projectRoot(project), nil)
	workflows := calls.NewCache(project.RootDir(), root, nil)
	process := analyzerhost.NewProcessPool(t.Context(), 1)
	session := &analysisSession{ctx: t.Context(), workingDir: root, reader: fsread.New(t.Context(), nil, nil), gitModes: &gitModes{}}
	for _, workflow := range []string{used, unused, used} {
		source, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		var rules []rule.Descriptor
		findings, _, err := session.check(workflow, source, project, nil, process, actions, workflows, &rules)
		if err != nil {
			t.Fatal(err)
		}
		if workflow == unused {
			if len(findings) != 0 {
				t.Fatalf("unreferenced metadata leaked into workflow: %+v", findings)
			}
		} else if len(findings) != 1 || findings[0].Rule != "inline-suppression" {
			t.Fatalf("metadata directive lost on repeated analysis: %+v", findings)
		}
	}
}
