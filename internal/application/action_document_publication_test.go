package application

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func virtualActionPaths(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.Mkdir(physical, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	manifest := filepath.Join(physical, "virtual", "action.yml")
	return root, manifest, filepath.Join(root, "alias", "virtual", "action.yml")
}

func TestActionOutlinePathIsIndependentOfPublicationOrder(t *testing.T) {
	_, physical, alias := virtualActionPaths(t)
	for _, concurrent := range []bool{false, true} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("concurrent=%t/reversed=%t", concurrent, reversed), func(t *testing.T) {
				paths := []string{physical, alias}
				if reversed {
					paths = []string{alias, physical}
				}
				documents := actionDocuments{byPath: map[string]documentmodel.Action{}}
				var workers sync.WaitGroup
				for _, path := range paths {
					if concurrent {
						workers.Go(func() { documents.add(documentmodel.Action{Path: path, Name: "virtual"}) })
					} else {
						documents.add(documentmodel.Action{Path: path, Name: "virtual"})
					}
				}
				workers.Wait()
				outlines := documents.list()
				if len(outlines) != 1 || outlines[0].DocumentPath() != alias {
					t.Fatalf("document spelling depends on publication order: %+v", outlines)
				}
			})
		}
	}
}

func TestVirtualActionOutlinesDeduplicateParentAliases(t *testing.T) {
	root, manifest, _ := virtualActionPaths(t)
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources: []sourceUnit{{
			Path: "workflow.yml", Project: workspace.Identity(root),
			Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/physical/virtual}, {uses: $/alias/virtual}]}}\n"),
		}},
		ReadFile: func(path string) ([]byte, error) {
			if fsread.PhysicalPath(path) == manifest {
				return []byte("name: virtual\ndescription: test\nruns: {using: composite, steps: []}\n"), nil
			}
			return nil, os.ErrNotExist
		},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("one virtual manifest produced duplicate outlines through parent aliases: %+v", result.Documents)
	}
	outline, ok := result.Documents[1].(documentmodel.Action)
	if !ok || outline.Name != "virtual" || outline.ParseStatus != "complete" || outline.Path != filepath.Join("alias", "virtual", "action.yml") {
		t.Fatalf("virtual action outline missing: %+v", result.Documents)
	}
}

func TestSharedActionOutlinesAreCompleteAfterBatch(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("broken=%t", broken), func(t *testing.T) {
			root := t.TempDir()
			manifest := "name: shared\ndescription: test\nruns: {using: composite, steps: []}\n"
			if broken {
				manifest = "runs: [\n"
			}
			writeShellcheckFixture(t, root, "shared/action.yml", manifest)
			var sources []sourceUnit
			for index := range 4 {
				name := fmt.Sprintf("unique-%d", index)
				writeShellcheckFixture(t, root, name+"/action.yml", "name: "+name+"\ndescription: test\nruns: {using: composite, steps: []}\n")
				sources = append(sources, sourceUnit{
					Path: fmt.Sprintf("workflow-%d.yml", index), Project: workspace.Identity(root),
					Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/shared}, {uses: $/" + name + "}]}}\n"),
				})
			}
			result, err := runTestAnalysis(t.Context(), analysisRequest{
				WorkingDir: root, Sources: sources,
				OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule { return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			outlines := map[string]documentmodel.Action{}
			for _, document := range result.Documents {
				if action, ok := document.(documentmodel.Action); ok {
					if _, duplicate := outlines[action.Path]; duplicate {
						t.Fatalf("duplicate action outline: %s", action.Path)
					}
					outlines[action.Path] = action
				}
			}
			if len(outlines) != 5 || len(result.Documents) != 9 {
				t.Fatalf("batch returned before its action outlines: actions=%d documents=%d", len(outlines), len(result.Documents))
			}
			for path, action := range outlines {
				if path == filepath.Join("shared", "action.yml") && broken {
					if action.ParseStatus == "complete" {
						t.Fatal("broken shared action reported as complete")
					}
					continue
				}
				if action.ParseStatus != "complete" || action.Name == "" {
					t.Fatalf("missing recovered declaration: %+v", action)
				}
			}
		})
	}
}
