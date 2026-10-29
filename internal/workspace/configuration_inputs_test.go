package workspace_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestAnalysisSelectedConfigInput(t *testing.T) {
	for _, names := range [][]string{nil, {"actionlint.yaml"}, {"actionlint.yml"}, {"actionlint.yaml", "actionlint.yml"}} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			config := t.TempDir()
			t.Setenv("GIT_CONFIG_GLOBAL", writeToolFixture(t, config, "gitconfig", ""))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("XDG_CONFIG_HOME", config)
			root := t.TempDir()
			writeToolRepository(t, root)
			exclude := writeToolFixture(t, root, ".git/info/exclude", "")
			physicalExclude, err := filepath.EvalSymlinks(exclude)
			if err != nil {
				t.Fatal(err)
			}
			workflow := writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
			want := []string{workflow, root, filepath.Join(root, ".github"), filepath.Join(root, ".github", "workflows"), physicalExclude, filepath.Join(filepath.Dir(filepath.Dir(physicalExclude)), "config")}
			for _, name := range names {
				if err := os.WriteFile(filepath.Join(root, ".github", name), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if len(names) > 0 {
				want = append(want, filepath.Join(root, ".github", names[0]))
			}
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Repository("")
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(want)
			slices.Sort(result.Inputs)
			if diff := cmp.Diff(want, result.Inputs); diff != "" {
				t.Fatalf("consumed inputs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnalysisSessionWorkflowDirectoryTypo(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".git", ".gihub/workflows", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: filepath.Join(root, "src")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Repository("")
	if result != nil || err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", filepath.Join(root, ".gihub", "workflows"))) || !strings.Contains(err.Error(), fmt.Sprintf("%q", filepath.Join(root, ".github", "workflows"))) {
		t.Fatalf("result=%v, error=%v", result, err)
	}
}

func TestAnalysisSessionConfigInputs(t *testing.T) {
	root := t.TempDir()
	workflowA := filepath.Join(root, "a", ".github", "workflows", "first.yml")
	workflowB := filepath.Join(root, "b", ".github", "workflows", "first.yml")
	secondB := filepath.Join(root, "b", ".github", "workflows", "second.yml")
	configA := filepath.Join(root, "a", ".github", "actionlint.yaml")
	configB := filepath.Join(root, "b", ".github", "actionlint.yml")
	loose := filepath.Join(root, "loose.yml")
	for _, name := range []string{"a", "b"} {
		for _, dir := range []string{".git", ".github/workflows"} {
			if err := os.MkdirAll(filepath.Join(root, name, dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, path := range []string{workflowA, workflowB, secondB, loose} {
		if err := os.WriteFile(path, []byte(toolWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{configA, configB} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		paths  []string
		inputs []string
	}{
		{"first project", []string{workflowA}, []string{workflowA, configA}},
		{"second project", []string{workflowB, secondB}, []string{workflowB, secondB, configB}},
		{"both projects", []string{workflowA, workflowB}, []string{workflowA, workflowB, configA, configB}},
		{"no project", []string{loose}, []string{loose}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.Files(tc.paths, nil)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(tc.inputs)
			slices.Sort(result.Inputs)
			if diff := cmp.Diff(tc.inputs, result.Inputs); diff != "" {
				t.Fatalf("analysis input mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnalysisSessionExplicitConfigInput(t *testing.T) {
	root := t.TempDir()
	workflow := filepath.Join(root, "workflow.yml")
	config := filepath.Join(root, "config.yml")
	for path, content := range map[string]string{
		workflow: toolWorkflow,
		config:   "{}\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(result.Inputs)
	want := []string{filepath.Clean(config), filepath.Clean(workflow)}
	slices.Sort(want)
	if diff := cmp.Diff(want, result.Inputs); diff != "" {
		t.Fatalf("explicit config input mismatch (-want +got):\n%s", diff)
	}
}

func TestAnalysisSessionEmpty(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, ".github", "actionlint.yaml")
	for _, dir := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Empty()
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{config}, result.Inputs); diff != "" {
		t.Fatalf("project config input mismatch (-want +got):\n%s", diff)
	}
	if len(result.Configurations) != 1 || result.Configurations[0].File != config || len(result.Sources) != 0 {
		t.Fatalf("project config report: %+v", result)
	}
	if result, err := session.Files(nil, nil); err != nil || len(result.Inputs) != 0 || len(result.Configurations) != 0 {
		t.Fatalf("an empty file selection discovered the project: %+v, %v", result, err)
	}

	outside := t.TempDir()
	explicit := filepath.Join(outside, "lint.yaml")
	if err := os.WriteFile(explicit, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err = workspace.New(t.Context(), workspace.Options{WorkingDir: outside, ConfigFile: explicit})
	if err != nil {
		t.Fatal(err)
	}
	result, err = session.Empty()
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{explicit}, result.Inputs); diff != "" {
		t.Fatalf("explicit config input mismatch (-want +got):\n%s", diff)
	}
	if len(result.Configurations) != 1 || result.Configurations[0].File != explicit || !result.Configurations[0].Explicit {
		t.Fatalf("explicit config report: %+v", result.Configurations)
	}

	session, err = workspace.New(t.Context(), workspace.Options{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := session.Empty(); err != nil || len(result.Inputs) != 0 || len(result.Configurations) != 0 {
		t.Fatalf("outside a project without a config: %+v, %v", result, err)
	}
}
