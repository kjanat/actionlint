// Package testutil provides source and filesystem fixtures for integration tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/results"
)

const (
	GoodWorkflow    = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\npermissions: {}\n"
	BadWorkflow     = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\npermissions: {}\n"
	WorkflowSteps   = "    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"
	CompositeAction = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
)

// ExecutableFixture creates a Git repository holding shell scripts with and
// without the executable bit, and returns its root and a function that runs
// git there. Its configuration disables the tools named by disabled.
func ExecutableFixture(t *testing.T, disabled ...string) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	command := func(args ...string) {
		t.Helper()
		if out, err := gitclient.Command(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	command("config", "core.filemode", "false")
	for _, name := range []string{"bad.sh", "good.sh", "scripts/bad.sh", "scripts/good.sh", "space dir/bad.sh", "{good,bad}.sh"} {
		fixturefiles.Write(t, root, name, "#!/bin/sh\necho script\n")
		command("add", "--", name)
		mode := "-x"
		if strings.HasSuffix(name, "good.sh") {
			mode = "+x"
		}
		command("update-index", "--chmod="+mode, "--", name)
	}
	fixturefiles.Write(t, root, "untracked.sh", "#!/bin/sh\n")
	fixturefiles.Write(t, root, "intent.sh", "#!/bin/sh\n")
	command("add", "-N", "intent.sh")
	config := make([]string, 0, len(disabled))
	for _, name := range disabled {
		config = append(config, name+": false")
	}
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {"+strings.Join(config, ", ")+"}\n")
	return root, command
}

// RequireTool skips the test unless the command of the bundled provider
// named name is on PATH, and returns the command's path. It clears environment
// variables such as SHELLCHECK_OPTS that add options to the provider's command.
func RequireTool(t *testing.T, name string) string {
	t.Helper()
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(manifests, func(m pluginmanifests.Manifest) bool { return m.Rule.Name == name })
	if index < 0 || manifests[index].Process == nil {
		t.Fatalf("no bundled process provider %s", name)
	}
	spec := manifests[index].Process
	path, err := exec.LookPath(spec.Entry)
	if err != nil {
		t.Skipf("%s is not installed", spec.Entry)
	}
	if spec.UserArgs != nil {
		for _, variable := range spec.UserArgs.EnvOptions {
			if os.Getenv(variable) != "" {
				t.Setenv(variable, "")
			}
		}
	}
	return path
}

func bundledEnvironmentOptions() ([]string, error) {
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		return nil, err
	}
	var variables []string
	for _, manifest := range manifests {
		if manifest.Process != nil && manifest.Process.UserArgs != nil {
			variables = append(variables, manifest.Process.UserArgs.EnvOptions...)
		}
	}
	return variables, nil
}

func AssertSourceFile(t *testing.T, got, want string) {
	t.Helper()
	actual, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("wrong sourced file: got %q, want %q", got, want)
	}
}

func FixtureSources(tb testing.TB, paths []string, project *workspace.Project, cfg *repoconfig.Config) []workspace.Source {
	tb.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]workspace.Source, 0, len(paths))
	reader := files.New(tb.Context(), nil, nil)
	for _, path := range paths {
		content, err := reader.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
		}
		display := path
		if project != nil && filepath.IsAbs(path) {
			if relative, err := filepath.Rel(project.RootDir(), path); err == nil {
				display = relative
			}
		}
		sources = append(sources, workspace.Source{Path: display, InputPath: path, Content: content, Project: project, Config: cfg})
	}
	return sources
}

// Main gives file-based suites a stable repository root, isolated Git settings
// and no host options for bundled tool providers.
func Main(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			panic(err)
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("cannot locate the test module root")
		}
		root = parent
	}
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	directory, err := os.MkdirTemp("", "actionlint-test-config-")
	if err != nil {
		panic(err)
	}
	global := filepath.Join(directory, "gitconfig")
	if err := os.WriteFile(global, nil, 0600); err != nil {
		panic(err)
	}
	for name, value := range map[string]string{"GIT_CONFIG_GLOBAL": global, "GIT_CONFIG_NOSYSTEM": "1", "XDG_CONFIG_HOME": directory} {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
	variables, err := bundledEnvironmentOptions()
	if err != nil {
		panic(err)
	}
	for _, variable := range variables {
		if err := os.Unsetenv(variable); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

// Diagnostics projects completed report values without exposing traversal state.
func Diagnostics(result *checking.Result, err error) ([]results.Diagnostic, error) {
	if err != nil {
		return nil, err
	}
	return result.Diagnostics, nil
}

// WorkflowDiagnostics checks a fixture with an explicit in-memory configuration.
func WorkflowDiagnostics(t *testing.T, source, config string) []results.Diagnostic {
	t.Helper()
	return WorkflowDiagnosticsWithOptions(t, source, config, checking.Options{})
}

func WorkflowDiagnosticsWithOptions(t *testing.T, source, config string, options checking.Options) []results.Diagnostic {
	t.Helper()
	var cfg *repoconfig.Config
	if config != "" {
		var err error
		cfg, err = checking.ConfigurationRules().Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := checking.Analyze(t.Context(), options, []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Config: cfg}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Diagnostics
}

// Composite writes a caller fixture and checks it through application input selection.
func Composite(t *testing.T, root, steps string, options checking.Options) *checking.Result {
	t.Helper()
	workflow := fixturefiles.Write(t, root, ".github/workflows/composite.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        shell: python
        working-directory: scripts
    steps:
      `+strings.ReplaceAll(steps, "\n", "\n      ")+"\npermissions: {}\n")
	options.WorkingDir = root
	result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
