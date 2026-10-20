package localapp

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/langserver"
)

func TestEditorAnalysisRetainsDependencySources(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	actionPath := filepath.Join(root, "local", "action.yml")
	if err := os.Mkdir(filepath.Dir(actionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	action := "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo \"::set-output name=result::value\"\n      shell: bash\n"
	if err := os.WriteFile(actionPath, []byte(action), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(root, "workflow.yml")
	documents := map[string]langserver.Document{
		workflow: {URI: langserver.FileURI(workflow), Version: 1, Text: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - uses: ./local\n"},
	}
	options := application.Options{WorkingDir: root, SkipProjectConfig: true}
	result, err := analyzeDocuments(t.Context(), options, documents)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "deprecated-commands" || result.Diagnostics[0].Path != actionPath {
		t.Fatalf("dependency finding lost: %+v", result.Diagnostics)
	}
	if result.Sources[actionPath] != action {
		t.Fatalf("dependency snapshot lost: %+v", result.Sources)
	}
	// Opening an unsaved, fixed action must override the disk dependency.
	documents[actionPath] = langserver.Document{URI: langserver.FileURI(actionPath), Version: 2, Text: "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo fixed\n      shell: bash\n"}
	result, err = analyzeDocuments(t.Context(), options, documents)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("open buffer did not override disk: %+v, %v", result.Diagnostics, err)
	}
}
