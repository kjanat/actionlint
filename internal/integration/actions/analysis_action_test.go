package actions_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"

	"golang.org/x/sys/execabs"
)

func TestStandaloneActionVirtualSourceThroughSymlink(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "real")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	source := strings.Replace(testutil.CompositeAction, "echo ok", "echo ${{ secrets.TOKEN }}", 1)
	for _, onDisk := range []bool{false, true} {
		if onDisk {
			testutil.WriteFixture(t, physical, "action.yml", source)
		}
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: alias}, []workspace.Source{{Path: "action.yml", Content: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "action.yml" {
			t.Fatalf("onDisk=%v: expected one finding in action.yml, got %+v", onDisk, result.Diagnostics)
		}
	}
}

func TestStandaloneActionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, source, message string
		line                  int
	}{
		{"clean", testutil.CompositeAction, "", 0},
		{"name", strings.Replace(testutil.CompositeAction, "name: example", "name: ''", 1), "name is required", 1},
		{"description", strings.Replace(testutil.CompositeAction, "description: example", "description: ''", 1), "description is required", 2},
		{"literal key", strings.Replace(testutil.CompositeAction, "description: example", "${{ 'description' }}: ''", 1), "description is required", 2},
		{"missing description", strings.Replace(testutil.CompositeAction, "description: example\n", "", 1), "description is required", 1},
		{"icon", testutil.CompositeAction + "branding:\n  icon: nonsense\n", "incorrect icon", 9},
		{"color", testutil.CompositeAction + "branding:\n  color: nonsense\n", "incorrect color", 9},
		{"using", strings.Replace(testutil.CompositeAction, "composite", "nonsense", 1), "invalid runner", 4},
		{"missing using", "name: x\ndescription: x\nruns: {}\n", "runs.using", 3},
		{"main", "name: x\ndescription: x\nruns:\n  using: node24\n  main: missing.js\n", "does not exist", 5},
		{"missing main", "name: x\ndescription: x\nruns:\n  using: node24\n", "is required", 3},
		{"invalid field", "name: x\ndescription: x\nruns:\n  using: composite\n  steps: []\n  main: invalid.js\n", "not allowed", 6},
		{"pre", "name: x\ndescription: x\nruns:\n  using: node24\n  main: main.js\n  pre-if: success()\n", "pre\" is required", 6},
		{"post", "name: x\ndescription: x\nruns:\n  using: node24\n  main: main.js\n  post-if: success()\n", "post\" is required", 6},
		{"input", testutil.CompositeAction + "inputs:\n  old:\n    deprecationMessage: ''\n", "deprecationMessage", 10},
		{"expression", strings.Replace(testutil.CompositeAction, "echo ok", "echo ${{ secrets.TOKEN }}", 1), "secrets", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			testutil.WriteFixture(t, root, "main.js", "")
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: "action.yml", Content: []byte(tc.source)}})
			if err != nil {
				t.Fatal(err)
			}
			outline, ok := result.Documents[0].(documentmodel.Action)
			if !ok || outline.ParseStatus() == "failed" {
				t.Fatalf("expected decoded action outline, got %#v", result.Documents)
			}
			if tc.message == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("clean manifest has findings: %+v", result.Diagnostics)
				}
				return
			}
			matches := slices.DeleteFunc(slices.Clone(result.Diagnostics), func(d diagmodel.Diagnostic) bool { return !strings.Contains(d.Message, tc.message) })
			if len(matches) != 1 || matches[0].Start.Line != tc.line || matches[0].Path != "action.yml" {
				t.Fatalf("want %q at action.yml:%d; got %+v (all: %+v)", tc.message, tc.line, matches, result.Diagnostics)
			}
		})
	}
}

func TestStandaloneActionMixedInputs(t *testing.T) {
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{
		{Path: "ci.yml", Content: []byte(testutil.GoodWorkflow)},
		{Path: "a/action.yml", Content: []byte(testutil.CompositeAction)},
		{Path: "b/action.yaml", Content: []byte("runs: [")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 3 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "syntax-check" {
		t.Fatalf("unexpected mixed result: %+v", result)
	}
	if _, ok := result.Documents[0].(documentmodel.Workflow); !ok {
		t.Fatalf("workflow routed as %T", result.Documents[0])
	}
	if outline, ok := result.Documents[2].(documentmodel.Action); !ok || outline.ParseStatus() != "failed" {
		t.Fatalf("broken action outline: %#v", result.Documents[2])
	}
}

func TestStandaloneActionWorkflowFilename(t *testing.T) {
	root := t.TempDir()
	for _, cwd := range []string{".", ".github", ".github/workflows"} {
		for _, name := range []string{"action.yml", "action.yaml"} {
			workingDir := filepath.Join(root, filepath.FromSlash(cwd))
			path, err := filepath.Rel(workingDir, filepath.Join(root, ".github", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: workingDir}, []workspace.Source{{Path: path, Content: []byte(testutil.GoodWorkflow)}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("cwd=%s path=%s: workflow failed: result=%+v err=%v", cwd, path, result, err)
			}
			if _, ok := result.Documents[0].(documentmodel.Workflow); !ok {
				t.Fatalf("cwd=%s path=%s: workflow routed to metadata parser: %#v", cwd, path, result.Documents)
			}
		}
	}
}

func TestStandaloneActionExecutableBitRequiresCaller(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	manifest := strings.Replace(testutil.CompositeAction, "echo ok", "./bad.sh", 1)
	testutil.WriteFixture(t, root, "action.yml", manifest)
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - uses: ./\n"
	for _, source := range []workspace.Source{{Path: "action.yml", Content: []byte(manifest)}, {Path: "ci.yml", Content: []byte(workflow)}} {
		source.Project = workspace.Identity(root)
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{source})
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, finding := range result.Diagnostics {
			found = found || finding.Rule == "executable-bit"
		}
		if want := source.Path == "ci.yml"; found != want {
			t.Fatalf("%s: executable-bit=%v, want %v; diagnostics=%+v", source.Path, found, want, result.Diagnostics)
		}
	}
}

func TestStandaloneActionNestedAndReferenced(t *testing.T) {
	root := t.TempDir()
	outer := strings.Replace(testutil.CompositeAction, "    - run: echo ok\n      shell: bash", "    - uses: ./inner", 1)
	inner := strings.Replace(testutil.CompositeAction, "echo ok", "echo '::set-output name=x::1'", 1)
	testutil.WriteFixture(t, root, "outer/action.yml", outer)
	testutil.WriteFixture(t, root, "inner/action.yml", inner)
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./outer\n"
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{
		{Path: "outer/action.yml", Content: []byte(outer), Project: workspace.Identity(root)},
		{Path: "ci.yml", Content: []byte(workflow), Project: workspace.Identity(root)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 3 {
		t.Fatalf("expected workflow and two distinct actions: %#v", result.Documents)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "deprecated-commands" || result.Diagnostics[0].Path != filepath.Join("inner", "action.yml") || result.Diagnostics[0].Start.Line != 6 {
		t.Fatalf("expected one nested script diagnostic: %+v", result.Diagnostics)
	}
}

func TestStandaloneActionSuppression(t *testing.T) {
	bad := strings.Replace(testutil.CompositeAction, "echo ok", "echo ${{ secrets.TOKEN }}", 1)
	for _, tc := range []struct{ name, source, config string }{
		{"inline", strings.Replace(bad, "    - run:", "    # actionlint:ignore-next-line action\n    - run:", 1), ""},
		{"file", bad, "ignores: [{rule: action, file: action.yml}]"},
		{"step", strings.Replace(bad, "      shell: bash", "      shell: bash\n      id: lint", 1), "ignores: [{rule: action, step: lint}]"},
		{"off", bad, "lint: {rules: {correctness: {action: off}}}"},
		{"override", bad, "overrides: [{includes: [action.yml], lint: {rules: {correctness: {action: off}}}}]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := repoconfig.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "action.yml", Content: []byte(tc.source), Config: config}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("suppression failed: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestStandaloneActionScriptTools(t *testing.T) {
	for _, tool := range []string{"shellcheck", "ruff"} {
		t.Run(tool, func(t *testing.T) {
			executable, err := execabs.LookPath(tool)
			if err != nil {
				t.Skipf("%s unavailable: %v", tool, err)
			}
			request := application.Options{WorkingDir: t.TempDir()}
			var requestSources []workspace.Source
			source := strings.Replace(testutil.CompositeAction, "echo ok", "echo $UNQUOTED", 1)
			if tool == "shellcheck" {
				request.Tools = registry.Registrations(registry.Selections{"shellcheck": {Command: executable}})
			} else {
				request.Tools = registry.Registrations(registry.Selections{"ruff": {Command: executable}})
				source = strings.Replace(testutil.CompositeAction, "echo ok", "print(UNDEFINED)", 1)
				source = strings.Replace(source, "shell: bash", "shell: python", 1)
			}
			requestSources = []workspace.Source{{Path: filepath.Join(request.WorkingDir, "action.yml"), Content: []byte(source)}}
			result, err := application.Analyze(t.Context(), request, requestSources)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == tool && d.Start.Line == 6 && d.Path == "action.yml"
			}) {
				t.Fatalf("missing %s at action.yml:6: %+v", tool, result.Diagnostics)
			}
		})
	}
}

func TestMixedActionShellcheckLevels(t *testing.T) {
	executable, err := execabs.LookPath("shellcheck")
	if err != nil {
		t.Skipf("ShellCheck unavailable: %v", err)
	}
	for _, level := range []string{"warn", "info", "off"} {
		t.Run(level, func(t *testing.T) {
			root := t.TempDir()
			cfg, err := repoconfig.Parse([]byte("overrides: [{includes: [inner/action.yml], lint: {rules: {external: {shellcheck: " + level + "}}}}]"))
			if err != nil {
				t.Fatal(err)
			}
			project := workspace.Identity(root)
			manifest := strings.Replace(testutil.CompositeAction, "echo ok", "echo $UNQUOTED", 1)
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n"
			result, err := application.Analyze(t.Context(), application.Options{
				WorkingDir: root,

				Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: executable}}),
			}, []workspace.Source{
				{Path: "ci.yml", Content: []byte(workflow), Project: project, Config: cfg},
				{Path: "inner/action.yml", Content: []byte(manifest), Project: project, Config: cfg},
			},
			)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if level == "off" {
				want = 0
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("want %d findings, got %+v", want, result.Diagnostics)
			}
			for _, diagnostic := range result.Diagnostics {
				severity := level
				if severity == "warn" {
					severity = "warning"
				}
				if diagnostic.Rule != "shellcheck" || diagnostic.Severity != severity || filepath.ToSlash(diagnostic.Path) != "inner/action.yml" {
					t.Errorf("want %s ShellCheck finding at inner/action.yml, got %+v", severity, diagnostic)
				}
			}
		})
	}
}
