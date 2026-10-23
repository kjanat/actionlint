package actions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestDockerfileOnlyActionInputsAndOutputs(t *testing.T) {
	for _, name := range []string{"Dockerfile", "dockerfile"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "action")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, name)
			if err := os.WriteFile(file, []byte(`FROM scratch
# actionlint:ignore definitely-invalid-rule
ENTRYPOINT ["/entrypoint.sh"]
`), 0o600); err != nil {
				t.Fatal(err)
			}
			content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: container
        uses: ./action
        with:
          args: --flag
          entrypoint: /custom-entrypoint
          undocumented: value
      - run: echo "$VALUE"
        env:
          VALUE: ${{ steps.container.outputs.unknown }}
`)
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: ".github/workflows/test.yml", Content: content, Project: workspace.Identity(root)}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("error=%v diagnostics=%+v", err, result.Diagnostics)
			}
			found := false
			for _, input := range result.Inputs {
				found = found || strings.EqualFold(input, file)
			}
			if !found {
				t.Fatalf("Dockerfile missing from analysis inputs: %v", result.Inputs)
			}
			if len(result.Documents) != 1 {
				t.Fatalf("Dockerfile created a manifest outline: %+v", result.Documents)
			}
		})
	}
}
