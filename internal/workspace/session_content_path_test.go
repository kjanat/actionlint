package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNamedContentDiscoversProjectRelativeToWorkingDirectory(t *testing.T) {
	processDir := t.TempDir()
	t.Chdir(processDir)
	repository := filepath.Join(processDir, "repo")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "ci.yml"), []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := New(Options{WorkingDir: "repo", SkipProjectConfig: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		path       string
		wantPath   string
		normalize  bool
		wantExists bool
	}{
		{name: "relative existing", path: "ci.yml", wantPath: "ci.yml", wantExists: true},
		{name: "absolute existing", path: filepath.Join(repository, "ci.yml"), wantPath: filepath.Join(repository, "ci.yml"), wantExists: true},
		{name: "relative missing", path: "missing.yml", wantPath: "missing.yml"},
		{name: "relative existing normalized", path: "ci.yml", wantPath: "ci.yml", normalize: true, wantExists: true},
		{name: "absolute existing normalized", path: filepath.Join(repository, "ci.yml"), wantPath: "ci.yml", normalize: true, wantExists: true},
		{name: "relative missing normalized", path: "missing.yml", wantPath: "missing.yml", normalize: true},
		{name: "absolute missing normalized", path: filepath.Join(repository, "missing.yml"), wantPath: "missing.yml", normalize: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection, err := session.Content(tc.path, []byte("on: push\n"), nil, tc.normalize)
			if err != nil {
				t.Fatal(err)
			}
			if len(selection.Sources) != 1 {
				t.Fatalf("sources=%+v", selection.Sources)
			}
			source := selection.Sources[0]
			if source.Path != tc.wantPath || source.InputPath != tc.path {
				t.Fatalf("display/input path=(%q,%q), want (%q,%q)", source.Path, source.InputPath, tc.wantPath, tc.path)
			}
			if tc.wantExists {
				if source.Project == nil || source.Project.RootDir() != repository {
					t.Fatalf("project=%v, want repository %q", source.Project, repository)
				}
				cached, err := session.ProjectAt(filepath.Join(repository, "ci.yml"))
				if err != nil || cached != source.Project {
					t.Fatalf("project cache identity=%p, source project=%p, err=%v", cached, source.Project, err)
				}
			} else if source.Project != nil {
				t.Fatalf("missing path unexpectedly discovered project %q", source.Project.RootDir())
			}
			wantInput := filepath.Join(repository, filepath.Base(tc.path))
			if len(selection.Inputs) != 1 || selection.Inputs[0] != wantInput {
				t.Fatalf("inputs=%v, want [%q]", selection.Inputs, wantInput)
			}
		})
	}
}
