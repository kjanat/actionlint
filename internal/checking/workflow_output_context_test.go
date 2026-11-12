package checking

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestWorkflowCallOutputContextUsesCapturedCalleeMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	directory := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	const callee = `on:
  workflow_call:
    outputs:
      artifact:
        value: ${{ jobs.build.outputs.artifact }}
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    outputs: {artifact: artifact}
    steps: [{run: echo ok}]
`
	if err := os.WriteFile(filepath.Join(directory, "callee.yml"), []byte(callee), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"artifact", "missing"} {
		t.Run(output, func(t *testing.T) {
			t.Parallel()
			caller := fmt.Sprintf(`on:
  workflow_call:
    outputs:
      published:
        value: ${{ jobs.call.outputs.%s }}
permissions: {}
jobs:
  call:
    uses: ./.github/workflows/callee.yml
`, output)
			path := filepath.Join(directory, "caller.yml")
			if err := os.WriteFile(path, []byte(caller), 0o600); err != nil {
				t.Fatal(err)
			}
			options := Options{WorkingDir: root, SkipProjectConfig: true, Tools: registry.Registrations(registry.Selections{
				"shellcheck": {Process: &processconfig.Options{Executable: new("")}},
				"ruff":       {Process: &processconfig.Options{Executable: new("")}},
			})}
			result, err := Analyze(t.Context(), options, []workspace.Source{{Path: path, Content: []byte(caller)}})
			if err != nil {
				t.Fatal(err)
			}
			if output == "artifact" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("declared output rejected: %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("expected the unknown output finding: %+v", result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			const access = "jobs.call.outputs.missing"
			before, _, found := strings.Cut(caller, access)
			if !found {
				t.Fatal("caller fixture does not contain the expected output access")
			}
			line, column := strings.Count(before, "\n")+1, len(before)-strings.LastIndex(before, "\n")
			if finding.Rule != "expression-reference" || finding.Code != "unknown-property" || finding.Start.Line != line || finding.Start.Column != column || finding.End.Line != line || finding.End.Column != column+len(access) || finding.Reference == nil || finding.Reference.Revision == "" || finding.Path != path {
				t.Fatalf("output finding lost its rule or captured member-access range: %+v reference=%+v", finding, finding.Reference)
			}
		})
	}
}
