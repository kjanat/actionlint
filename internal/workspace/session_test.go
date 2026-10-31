package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestSelectionPreparesSourcesWithoutParsing(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['excluded.yml']}\n")
	var reads []string
	session, err := New(t.Context(), Options{WorkingDir: root, ReadFile: func(path string) ([]byte, error) {
		reads = append(reads, path)
		if filepath.Base(path) == "broken.yml" {
			return []byte("[invalid yaml"), nil
		}
		if filepath.Base(path) == "excluded.yml" {
			t.Fatal("excluded input was read")
		}
		return os.ReadFile(path)
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := session.Files([]string{filepath.Join(root, "excluded.yml"), filepath.Join(root, "broken.yml")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Sources) != 1 || selected.Sources[0].Path != "broken.yml" || string(selected.Sources[0].Content) != "[invalid yaml" {
		t.Fatalf("selection=%+v", selected)
	}
	if len(selected.Configurations) != 1 || selected.Configurations[0].Project != root {
		t.Fatalf("configurations=%+v", selected.Configurations)
	}
	for _, path := range []string{filepath.Join(root, "broken.yml"), filepath.Join(root, ".github/actionlint.yaml")} {
		if !slices.Contains(selected.Inputs, path) {
			t.Errorf("input %q missing from selected config: %v", path, selected.Inputs)
		}
	}
	if !slices.Contains(reads, filepath.Join(root, "broken.yml")) {
		t.Fatal("selected source was not read")
	}
}

func TestSelectionProvenanceDoesNotLeakAcrossBatches(t *testing.T) {
	session, err := New(t.Context(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
		writeShellcheckFixture(t, root, ".github/actionlint.yaml", "config-variables: [LOCAL]\n")
		writeShellcheckFixture(t, root, "workflow.yml", "[not parsed here")
		selected, err := session.Files([]string{filepath.Join(root, "workflow.yml")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(selected.Configurations) != 1 || selected.Configurations[0].Project != root {
			t.Fatalf("configurations=%+v", selected.Configurations)
		}
		for _, path := range selected.Inputs {
			if !strings.HasPrefix(path, root+string(filepath.Separator)) {
				t.Errorf("previous batch input leaked: %s", path)
			}
		}
	}
}

func TestNamedContentTracksPathRelativeToWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	session, err := New(t.Context(), Options{WorkingDir: root, SkipProjectConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := session.Content("named-input.yml", []byte("on: push"), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Sources[0].Path != "named-input.yml" {
		t.Fatalf("display path=%q", selected.Sources[0].Path)
	}
	if !slices.Equal(selected.Inputs, []string{filepath.Join(root, "named-input.yml")}) {
		t.Fatalf("inputs=%v", selected.Inputs)
	}
}

func TestOptionsConfigReplacesRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['workflow.yml']}\n")
	config := &repoconfig.Config{}
	session, err := New(t.Context(), Options{WorkingDir: root, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := session.Content(filepath.Join(root, "workflow.yml"), []byte("on: push"), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := selected.Sources[0].Config; got != config {
		t.Fatalf("source config=%p, want the Options.Config %p", got, config)
	}
	if _, err := New(t.Context(), Options{WorkingDir: root, Config: config, ConfigFile: "actionlint.yaml"}); err == nil {
		t.Fatal("Config together with ConfigFile was accepted")
	}
}
