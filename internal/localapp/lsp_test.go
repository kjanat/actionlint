package localapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/sourceid"
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

func TestEditorExcludedDependencyUsesUnsavedCapturedBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	actionPath := filepath.Join(root, "local", "action.yml")
	if err := os.Mkdir(filepath.Dir(actionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actionPath, []byte("name: disk\ndescription: clean\nruns: {using: composite, steps: [{shell: bash, run: echo disk}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := repoconfig.ParseOverlay("config", []byte("files: {excludes: ['local/**']}\n"))
	if err != nil {
		t.Fatal(err)
	}
	unsaved := "\ufeffname: unsaved\ndescription: captured\n# 😀 open dependency\nruns:\n  using: composite\n  steps:\n    - run: echo \"😀::set-output name=result::unsaved\"\n      shell: bash\n"
	workflow := filepath.Join(root, "workflow.yml")
	documents := map[string]langserver.Document{
		workflow:                             {URI: langserver.FileURI(workflow), Version: 3, Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./local}]}}\n"},
		filepath.Join("local", "action.yml"): {URI: langserver.FileURI(actionPath), Version: 9, Text: unsaved},
	}
	var actionReads atomic.Int32
	result, err := analyzeDocuments(t.Context(), application.Options{WorkingDir: root, SkipProjectConfig: true, ConfigOverlays: []repoconfig.Overlay{overlay}, ReadFile: func(path string) ([]byte, error) {
		if filepath.Clean(path) == actionPath {
			actionReads.Add(1)
		}
		return os.ReadFile(path)
	}}, documents)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("excluded open dependency was not analyzed through its caller: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Path != actionPath || finding.Rule != "deprecated-commands" || finding.Start.Line != 7 || finding.Reference == nil || finding.Reference.Revision != sourceid.Digest([]byte(unsaved)) {
		t.Fatalf("dependency diagnostic used different bytes or coordinates: %+v", finding)
	}
	if result.Sources[actionPath] != unsaved || actionReads.Load() != 0 {
		t.Fatalf("open dependency escaped capture: text=%q backing reads=%d", result.Sources[actionPath], actionReads.Load())
	}
}

func TestEditorConflictingAliasesWithholdOnlyConflictedSource(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	actionPath := filepath.Join(root, "local", "action.yml")
	alias := filepath.Dir(actionPath) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(actionPath)
	workflow := filepath.Join(root, "workflow.yml")
	good := filepath.Join(root, "good.yml")
	goodText := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo ::set-output name=result::good'}]}}\n"
	first := "name: first\ndescription: first\nruns: {using: composite, steps: [{shell: bash, run: echo first}]}\n"
	second := strings.ReplaceAll(first, "first", "second")
	for _, reverse := range []bool{false, true} {
		a, b := first, second
		if reverse {
			a, b = b, a
		}
		documents := map[string]langserver.Document{
			workflow: {Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./local}]}}\n"},
			good:     {Text: goodText}, actionPath: {Text: a}, alias: {Text: b},
		}
		result, err := analyzeDocuments(t.Context(), application.Options{WorkingDir: root, SkipProjectConfig: true}, documents)
		if _, conflicted := errors.AsType[*capture.ConflictError](err); !conflicted {
			t.Fatalf("alias conflict lost typed cause: %v", err)
		}
		if _, exposed := result.Sources[actionPath]; exposed {
			t.Fatal("conflicted bytes were offered for editor coordinates")
		}
		if result.Sources[good] != goodText {
			t.Fatal("unrelated captured source was discarded")
		}
		found := false
		for _, finding := range result.Diagnostics {
			if finding.Path == actionPath || finding.Path == alias {
				t.Fatalf("conflicted declaration published a finding: %+v", finding)
			}
			found = found || finding.Path == good && finding.Rule == "deprecated-commands"
		}
		if !found {
			t.Fatalf("unrelated finding was discarded: %+v", result.Diagnostics)
		}
	}
}
