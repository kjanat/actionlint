package actions_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestLocalActionSourceConfig(t *testing.T) {
	config, err := repoconfig.Parse([]byte("local-actions: {'$/generated/': './source'}\noverrides:\n  - includes: ['special.yml']\n    local-actions: {'./generated': './other'}\n  - includes: ['reset.yml']\n    local-actions: null\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{{"ordinary.yml", "./source"}, {"special.yml", "./other"}, {"reset.yml", ""}} {
		got, err := repoconfig.ForFile(config, tc.path, "")
		if err != nil || got.LocalActions["./generated"] != tc.want {
			t.Fatalf("%s: %+v %v", tc.path, got, err)
		}
	}
	if config.LocalActions["./generated"] != "./source" {
		t.Fatal("override mutated shared config")
	}
	programmatic := &repoconfig.Config{LocalActions: repoconfig.LocalActionSources{"$/generated/": "$/source"}, Overrides: []repoconfig.Override{{Includes: []string{"special.yml"}, LocalActions: repoconfig.LocalActionSources{"$/generated": "$/other"}}}}
	got, err := repoconfig.ForFile(programmatic, "special.yml", "")
	if err != nil || got.LocalActions["./generated"] != "./other" {
		t.Fatalf("programmatic mappings: %+v %v", got, err)
	}
	for _, value := range []string{"{'./generated': '../outside'}", "{'./generated': 42}", "{'./generated': './source', '$/generated': './other'}", "{'./generated': '${{ inputs.path }}'}", "[]"} {
		if _, err := repoconfig.Parse([]byte("local-actions: " + value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	root := t.TempDir()
	testutil.WriteFixture(t, root, "base.yml", "local-actions: {'./generated': './source', './second': './two'}\n")
	file := testutil.WriteFixture(t, root, "config.yml", "extends: [base.yml]\nlocal-actions: {'$/generated/': './override'}\n")
	merged, err := repoconfig.Load(file)
	if err != nil || merged.LocalActions["./generated"] != "./override" || merged.LocalActions["./second"] != "./two" {
		t.Fatalf("extends: %+v %v", merged, err)
	}
}

func TestLocalActionSourceValidation(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './source'}\n")
	testutil.WriteFixture(t, root, "source/action.yml", `name: source
description: source
inputs:
  token:
    description: token
    required: true
outputs:
  result:
    description: result
runs:
  using: composite
  steps:
    - uses: actions/checkout
`)
	result := testutil.Composite(t, root, "- uses: ./generated\n  id: generated\n  with:\n    unexpected: value\n- run: echo '${{ steps.generated.outputs.unknown }}'", application.Options{})
	for _, want := range []struct{ rule, message string }{{"action", "token"}, {"action", "unexpected"}, {"expression-reference", "unknown"}} {
		if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
			return d.Rule == want.rule && strings.Contains(d.Message, want.message)
		}) {
			t.Fatalf("missing %s/%s: %+v", want.rule, want.message, result.Diagnostics)
		}
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "action" && filepath.ToSlash(d.Path) == "source/action.yml" && d.Start.Line == 13
	}) {
		t.Fatalf("lost source diagnostic: %+v", result.Diagnostics)
	}
}

func TestLocalActionSourceFailureReportedOncePerReference(t *testing.T) {
	for _, failure := range []string{"missing", "directory-symlink", "manifest-symlink"} {
		t.Run(failure, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './source'}\n")
			if failure != "missing" {
				outside := t.TempDir()
				manifest := testutil.WriteFixture(t, outside, "action.yml", "name: Outside\n")
				target, link := outside, filepath.Join(root, "source")
				if failure == "manifest-symlink" {
					if err := os.Mkdir(link, 0o755); err != nil {
						t.Fatal(err)
					}
					target, link = manifest, filepath.Join(link, "action.yml")
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skip(err)
				}
			}
			result := testutil.Composite(t, root, "- uses: ./generated\n  id: first\n- uses: ./generated\n- uses: ./generated\n  id: third", application.Options{})
			if len(result.Diagnostics) != 3 {
				t.Fatalf("want one failure per reference: %+v", result.Diagnostics)
			}
			for _, diagnostic := range result.Diagnostics {
				wantRule := "action"
				if failure == "missing" {
					wantRule = "missing-local-action"
				}
				if diagnostic.Rule != wantRule || !strings.Contains(diagnostic.Message, "source mapping") {
					t.Fatalf("unexpected diagnostic: %+v", diagnostic)
				}
			}
		})
	}
}

func TestMissingLocalActionRuntimeCheckout(t *testing.T) {
	for _, tc := range []struct {
		name, checkout string
		missing        bool
	}{
		{"private", "- uses: actions/checkout@v6\n  with:\n    repository: ${{ secrets.PRIVATE_REPOSITORY }}\n    token: ${{ secrets.PRIVATE_TOKEN }}", false},
		{"conditional", "- uses: actions/checkout@v6\n  if: github.event_name == 'push'", false},
		{"continue-on-error", "- uses: actions/checkout@v6\n  continue-on-error: true", false},
		{"known", "- uses: actions/checkout@v6", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			result := testutil.Composite(t, root, tc.checkout+"\n- uses: ./private-action", application.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "missing-local-action" })
			if found != tc.missing {
				t.Fatalf("missing=%t, expected=%t: %+v", found, tc.missing, result.Diagnostics)
			}
		})
	}
}

func TestMissingLocalActionSourceSuppression(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
		for _, ignored := range []bool{false, true} {
			root, _ := testutil.ExecutableFixture(t)
			config, err := repoconfig.Parse([]byte("local-actions: {'./generated': './absent-source'}\n"))
			if err != nil {
				t.Fatal(err)
			}
			comment := ""
			if ignored {
				comment = " # actionlint:ignore missing-local-action"
			}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./generated" + comment + "\n")
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: ".github/workflows/test.yml", Content: content, Config: config, Project: workspace.Identity(root)}})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if ignored {
				want = 0
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("%s ignored=%t: %+v", runner, ignored, result.Diagnostics)
			}
			if !ignored && (result.Diagnostics[0].Rule != "missing-local-action" || strings.Contains(result.Diagnostics[0].Message, root)) {
				t.Fatalf("%s: %+v", runner, result.Diagnostics)
			}
		}
	}
}

func TestInsertedCheckoutLocation(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	for _, tc := range []struct {
		name, with string
		known      bool
	}{
		{"literal", "{'${{ insert }}': {path: vendor}}", true},
		{"repeated", "{'${{ insert }}': {path: vendor}, '${{ insert }}': {clean: true}}", true},
		{"nested", "{'${{ insert }}': {'${{ insert }}': {path: vendor}}}", true},
		{"constant object", `{'${{ insert }}': "${{ fromJSON('{\"path\":\"vendor\"}') }}"}`, true},
		{"whole constant object", `${{ fromJSON('{"path":"vendor"}') }}`, true},
		{"literal expression", `{'${{ insert }}': {path: "${{ 'vendor' }}"}}`, true},
		{"unknown name", "{'${{ insert }}': {'${{ vars.NAME }}': vendor}}", false},
		{"known name", `{'${{ insert }}': {"${{ 'path' }}": vendor}}`, true},
		{"unknown object", "{'${{ insert }}': '${{ fromJSON(vars.INPUTS) }}'}", false},
		{"unknown path", "{'${{ insert }}': {path: '${{ vars.PATH }}'}}", false},
		{"unknown sibling", "{path: vendor, '${{ insert }}': '${{ fromJSON(vars.INPUTS) }}'}", false},
		{"foreign", "{'${{ insert }}': {path: vendor, repository: other/repo}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: "+tc.with+"\n- uses: ./vendor/missing\n- run: ./vendor/bad.sh", application.Options{})
			for _, rule := range []string{"missing-local-action", "executable-bit"} {
				found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == rule })
				// The unknown local action invalidates executable state; test that rule independently.
				if rule == "executable-bit" {
					script := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: "+tc.with+"\n- run: ./vendor/bad.sh\n  shell: bash\n  working-directory: .", application.Options{})
					found = slices.ContainsFunc(script.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == rule })
				}
				if found != tc.known {
					t.Fatalf("%s finding=%v, want=%v: %+v", rule, found, tc.known, result.Diagnostics)
				}
			}
		})
	}
}

func TestConstantEnvironmentInsertionCheckout(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	for _, tc := range []struct {
		name, env string
		known     bool
	}{
		{"constant", `${{ fromJSON('{"VALUE":"text"}') }}`, true},
		{"nested", `{'${{ insert }}': "${{ fromJSON('{\"VALUE\":\"text\"}') }}"}`, true},
		{"unknown", `${{ fromJSON(vars.ENV) }}`, false},
		{"loader", `${{ fromJSON('{"LD_PRELOAD":"custom.so"}') }}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout := "- uses: actions/checkout@v6\n  env:\n    ${{ insert }}: " + tc.env + "\n"
			for _, tcRule := range []struct{ rule, step string }{
				{"missing-local-action", "- uses: ./missing"},
				{"executable-bit", "- run: ./bad.sh\n  shell: bash\n  working-directory: ."},
			} {
				result := testutil.Composite(t, root, checkout+tcRule.step, application.Options{})
				found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == tcRule.rule })
				if found != tc.known {
					t.Fatalf("%s finding=%v, want=%v: %+v", tcRule.rule, found, tc.known, result.Diagnostics)
				}
			}
		})
	}
}
