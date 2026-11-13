package checking

import (
	"os"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcemodel "actionlint.kjanat.dev/v2/internal/input/source"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
)

func TestLocalActionSuppressionSourcesStayWorkflowScoped(t *testing.T) {
	t.Parallel()
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
permissions: {}
`)
	unused := writeShellcheckFixture(t, root, ".github/workflows/unused.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`)
	project := workspace.Identity(root)
	actions := workspaceactions.NewCache(t.Context(), projectRoot(project), nil)
	workflows := workflowinputs.NewCache(project.RootDir(), nil)
	process := hostprocess.NewProcessPool(t.Context(), 1)
	session := &analysisSession{ctx: t.Context(), workingDir: root, reader: files.New(t.Context(), nil, nil), gitModes: &gitModes{}}
	var declarations *capturedSnapshots
	session.snapshots, session.captures, declarations = newCapturedSnapshots(t, root)
	actions.Configure(workspaceactions.Options{Captures: session.captures, Context: t.Context(), WorkingDir: root, Reader: session.reader})
	workflows.SetSources(t.Context(), session.captures, root)
	for _, workflow := range []string{used, unused, used} {
		source, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		var rules []rule.Descriptor
		captured, err := declarations.Capture(workflow, source, sourcemodel.YAML)
		if err != nil {
			t.Fatal(err)
		}
		findings, _, err := session.check(workflow, workflow, captured, project, nil, repoconfig.Provenance{}, process, actions, workflows, &rules)
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
