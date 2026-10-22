package documentparse

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/sourceid"
)

func TestActionFactsDoNotBecomeWorkflowTraversalInputs(t *testing.T) {
	source := []byte("name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n")
	document := Action("action.yml", "/project/action.yml", source)
	if !document.CanAnalyze() || document.Metadata == nil || document.Workflow != nil {
		t.Fatalf("action entered workflow representation: %+v", document)
	}
	facts := document.Facts()
	if facts == nil || len(facts.Jobs[""].Steps) != 1 || document.Facts() != facts {
		t.Fatal("action facts are missing or unstable across consumers")
	}
	refs := References("/project/action.yml", source, document, nil, filepath.Clean)
	step := facts.Jobs[""].Steps[0]
	if refs["/project/action.yml"].Steps[step].Address != "runs.steps[0]" {
		t.Fatal("fact projections lost their action source identity")
	}
}

func TestReferenceIdentitiesPreserveSourceLookupPaths(t *testing.T) {
	workflow := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./action\n")
	action := []byte("name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n")
	identify := func(path string) string { return "/physical/" + filepath.Base(path) }
	refs := References("/alias/ci.yml", workflow, Workflow("ci.yml", workflow),
		map[string][]byte{"/alias/action.yml": action}, identify)
	for _, path := range []string{"/alias/ci.yml", "/alias/action.yml"} {
		index, ok := refs[path]
		if !ok {
			t.Fatalf("caller source lookup path %q lost", path)
		}
		want := sourceid.Document(identify(path))
		if index.Document.Document != want {
			t.Fatalf("document identity = %q, want %q", index.Document.Document, want)
		}
		if len(index.Positions) == 0 {
			t.Fatalf("source %q has no node references", path)
		}
		for _, reference := range index.Positions {
			if reference.Document != want {
				t.Fatalf("node document identity = %q, want %q", reference.Document, want)
			}
		}
	}
}
