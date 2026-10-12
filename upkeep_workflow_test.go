package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestUpkeepGoToolchain(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required for the Ubuntu upkeep workflow")
	}
	data, err := os.ReadFile(".github/workflows/upkeep.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string
				Run  string
				With struct {
					AddPaths string `yaml:"add-paths"`
				}
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	var paths []string
	for _, step := range workflow.Jobs["go-toolchain"].Steps {
		if step.Name == "Check the latest stable Go toolchain" {
			script = step.Run
		}
		if step.With.AddPaths != "" {
			paths = strings.Fields(step.With.AddPaths)
		}
	}
	if script == "" || !slices.Equal(paths, []string{"go.mod", "Dockerfile", "CONTRIBUTING.md", "flake.nix", "flake.lock"}) {
		t.Fatalf("missing update step or incomplete PR paths: %v", paths)
	}
	original := make(map[string]string, len(paths))
	for _, name := range paths {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		original[name] = string(data)
	}
	for _, version := range []string{"go1.27.2", "go1.27.3", "go1.28.0", "go1.28rc1", "invalid"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range original {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The Node suite exercises synchronized edits and Nix validation.
			// Here, verify the workflow validates releases and delegates correctly.
			stubs := `curl() { printf '%s\n' "$UPKEEP_TEST_VERSION"; }
node() {
  [[ "$1" == scripts/update-go-toolchain.mjs && "$2" == "$UPKEEP_TEST_VERSION" ]] || return 1
  printf 'updater:%s\n' "$2"
}
`
			cmd := exec.CommandContext(t.Context(), "bash", "-c", stubs+script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "UPKEEP_TEST_VERSION="+version, "GITHUB_OUTPUT="+filepath.Join(dir, "output"))
			output, err := cmd.CombinedOutput()
			invalid := version == "go1.28rc1" || version == "invalid"
			if (err != nil) != invalid {
				t.Fatalf("update failed: %v\n%s", err, output)
			}
			if strings.Contains(string(output), "updater:"+version) == invalid {
				t.Fatalf("unexpected updater invocation: %s", output)
			}
			for _, name := range paths {
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != original[name] {
					t.Errorf("workflow changed %s outside the verified updater", name)
				}
			}
		})
	}
}
