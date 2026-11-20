package checking

import (
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/results/policy"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
)

func TestLocalActionSuppressionSourcesStayWorkflowScoped(t *testing.T) {
	t.Parallel()
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/index.js", "console.log('ok');\n")
	writeShellcheckFixture(t, root, "local/action.yml", `name: local # actionlint:ignore syntax-check --
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
	actions := localactions.NewManifestCache(t.Context(), projectRoot(project), nil)
	workflows := reusableworkflows.NewMetadataCache(project.RootDir(), nil)
	process := hostprocess.NewProcessPool(t.Context(), 1)
	session := &analysisSession{ctx: t.Context(), workingDir: root, reader: files.New(t.Context(), nil, nil), gitModes: &gitModes{}, checks: testChecks(t, nil, nil)}
	var fixture *declarations.CapturedSnapshots
	session.snapshots, session.captures, fixture = newCapturedSnapshots(t, root)
	actions.Configure(localactions.Options{Captures: session.captures, Context: t.Context(), WorkingDir: root, Reader: session.reader})
	workflows.SetSources(t.Context(), session.captures, root)
	for _, workflow := range []string{used, unused, used} {
		source, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		var rules []rule.Descriptor
		captured, err := fixture.Capture(workflow, source, sourcetext.YAML)
		if err != nil {
			t.Fatal(err)
		}
		findings, _, batch, err := session.check(workflow, workflow, captured, project, nil, configuration.Provenance{}, process, actions, workflows, &rules)
		if err != nil {
			t.Fatal(err)
		}
		batch.Findings, batch.InputPath = findings, workflow
		published, err := policy.Publish([]policy.Batch{*batch}, session.checkCompletion.incompleteChecks())
		if err != nil {
			t.Fatal(err)
		}
		findings = published.Findings[0]
		if workflow == unused {
			if len(findings) != 0 {
				t.Fatalf("unreferenced metadata leaked into workflow: %+v", findings)
			}
		} else if len(findings) != 1 || findings[0].Rule != "inline-suppression" {
			t.Fatalf("metadata directive lost on repeated analysis: %+v", findings)
		}
	}
}
