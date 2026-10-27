package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestManifestAliasesStandaloneAndNested(t *testing.T) {
	for _, filename := range []string{"action.yml", "action.yaml"} {
		for _, entry := range []string{"action", "workflow"} {
			t.Run(filename+"/"+entry, func(t *testing.T) {
				root := t.TempDir()
				manifest := "name: &label example\ndescription: *label\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
				testutil.WriteFixture(t, root, "inner/"+filename, manifest)
				outer := strings.Replace(testutil.CompositeAction, "    - run: echo ok\n      shell: bash", "    - uses: ./inner", 1)
				testutil.WriteFixture(t, root, "outer/action.yml", outer)
				source := workspace.Source{Path: "outer/action.yml", Content: []byte(outer), Project: workspace.Identity(root)}
				if entry == "workflow" {
					source.Path = "ci.yml"
					source.Content = []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./outer\n")
				}
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{source})
				if err != nil {
					t.Fatal(err)
				}
				var got []diagmodel.Position
				for _, d := range result.Diagnostics {
					if !strings.Contains(d.Message, "YAML anchors") && !strings.Contains(d.Message, "YAML aliases") {
						continue
					}
					if d.Rule != "action" || d.Path != filepath.Join("inner", filename) {
						t.Fatalf("nested manifest diagnostic points outside its source: %+v", d)
					}
					got = append(got, d.Start)
				}
				if len(got) != 2 || got[0] != (diagmodel.Position{Line: 1, Column: 7}) || got[1] != (diagmodel.Position{Line: 2, Column: 14}) {
					t.Fatalf("manifest anchor/alias diagnostics = %v; all diagnostics: %+v", got, result.Diagnostics)
				}
			})
		}
	}
}

func TestWorkflowAliasesRemainSupported(t *testing.T) {
	source := "on: push\njobs:\n  first:\n    runs-on: ubuntu-latest\n    env: &env {VALUE: example}\n    steps: [{run: echo ok}]\n  second:\n    runs-on: ubuntu-latest\n    env: *env\n    steps: [{run: echo ok}]\n"
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("workflow aliases rejected: error=%v diagnostics=%+v", err, result.Diagnostics)
	}
}
