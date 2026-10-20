package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestLocalActionSourceCustomReader(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
		t.Run(runner, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './virtual'}\n")
			manifest := filepath.Join(root, "virtual", "action.yml")
			reader := func(path string) ([]byte, error) {
				if path == manifest {
					return []byte("name: Virtual\ndescription: Virtual\noutputs:\n  result:\n    description: Result\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"), nil
				}
				return os.ReadFile(path)
			}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./generated\n        id: generated\n      - run: echo '${{ steps.generated.outputs.result }}'\n        shell: bash\n")
			config, err := repoconfig.Parse([]byte("local-actions: {'./generated': './virtual'}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, ReadFile: reader}, []workspace.Source{{Path: ".github/workflows/test.yml", Content: content, Config: config, Project: workspace.Identity(root)}})
			if err != nil || len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, manifest) {
				t.Fatalf("direct analysis: %+v, %v", result, err)
			}
			workflow := writeFixture(t, root, ".github/workflows/test.yml", string(content))
			session := application.Options{WorkingDir: root, ReadFile: reader}

			result, err = application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil || len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, manifest) {
				t.Fatalf("session analysis: %+v, %v", result, err)
			}
		})
	}
}
