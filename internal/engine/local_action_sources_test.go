package engine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLocalActionSourceMappings(t *testing.T) {
	root := t.TempDir()
	first := writeShellcheckFixture(t, root, "source/action.yml", "name: Source\ndescription: Source\ninputs:\n  token:\n    description: Token\n    required: true\noutputs:\n  result:\n    description: Result\nruns:\n  using: composite\n  steps:\n    - run: echo test\n      shell: bash\n")
	writeShellcheckFixture(t, root, "other/action.yml", "name: Other\ndescription: Other\nruns: {using: composite, steps: []}\n")
	base := NewLocalActionsCache(&Project{root: root}, nil)
	view := &LocalActionsCache{base: base, sourceMappings: LocalActionSources{"./generated": "./source"}, usedSources: map[string][]byte{}}
	for _, spec := range []string{"./generated", "$/generated", "./generated/../generated"} {
		meta, _, err := view.FindMetadata(spec)
		if err != nil || meta == nil {
			t.Fatalf("%s: %v, %v", spec, meta, err)
		}
		if meta.Path() != first || !meta.sourceMapped || meta.Inputs["token"] == nil || meta.Outputs["result"] == nil {
			t.Fatalf("wrong mapped metadata: %+v", meta)
		}
		if _, ok := view.usedSources[first]; !ok {
			t.Fatal("source manifest not recorded")
		}
	}
	second := &LocalActionsCache{base: base, sourceMappings: LocalActionSources{"./generated": "./other"}}
	meta, _, err := second.FindMetadata("./generated")
	if err != nil || meta.Name != "Other" {
		t.Fatalf("mapping leaked between views: %+v %v", meta, err)
	}
	plain, _, err := base.FindMetadata("./source")
	if err != nil || plain.sourceMapped {
		t.Fatalf("base metadata changed: %+v %v", plain, err)
	}
	if _, _, err := base.FindMetadata("./generated"); err == nil {
		t.Fatal("mapping leaked into base")
	}
	if _, _, err := view.FindMetadata("./generated/nested"); err == nil {
		t.Fatal("mapping unexpectedly matched a child directory")
	}
	view.setCheckout(runDirectory{kind: directoryKnown, path: "checkout"})
	if _, _, err := view.FindMetadata("./checkout/generated"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := view.FindMetadata("$/generated"); err != nil {
		t.Fatal(err)
	}
	view.restoreCheckout(&checkoutPlacement{directory: runDirectory{kind: directoryKnown, path: "."}, foreign: true})
	if meta, _, err := view.FindMetadata("./generated"); err != nil || meta != nil {
		t.Fatalf("mapping resolved a foreign checkout: %+v %v", meta, err)
	}
}

func TestLocalActionSourceConfig(t *testing.T) {
	config, err := ParseConfig([]byte("local-actions: {'$/generated/': './source'}\noverrides:\n  - includes: ['special.yml']\n    local-actions: {'./generated': './other'}\n  - includes: ['reset.yml']\n    local-actions: null\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{{"ordinary.yml", "./source"}, {"special.yml", "./other"}, {"reset.yml", ""}} {
		got, err := configForFile(config, tc.path, "")
		if err != nil || got.LocalActions["./generated"] != tc.want {
			t.Fatalf("%s: %+v %v", tc.path, got, err)
		}
	}
	if config.LocalActions["./generated"] != "./source" {
		t.Fatal("override mutated shared config")
	}
	programmatic := &Config{LocalActions: LocalActionSources{"$/generated/": "$/source"}, Overrides: []ConfigOverride{{Includes: []string{"special.yml"}, LocalActions: LocalActionSources{"$/generated": "$/other"}}}}
	got, err := configForFile(programmatic, "special.yml", "")
	if err != nil || got.LocalActions["./generated"] != "./other" {
		t.Fatalf("programmatic mappings: %+v %v", got, err)
	}
	for _, value := range []string{"{'./generated': '../outside'}", "{'./generated': 42}", "{'./generated': './source', '$/generated': './other'}", "{'./generated': '${{ inputs.path }}'}", "[]"} {
		if _, err := ParseConfig([]byte("local-actions: " + value)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	root := t.TempDir()
	writeShellcheckFixture(t, root, "base.yml", "local-actions: {'./generated': './source', './second': './two'}\n")
	file := writeShellcheckFixture(t, root, "config.yml", "extends: [base.yml]\nlocal-actions: {'$/generated/': './override'}\n")
	merged, err := ReadConfigFile(file)
	if err != nil || merged.LocalActions["./generated"] != "./override" || merged.LocalActions["./second"] != "./two" {
		t.Fatalf("extends: %+v %v", merged, err)
	}
}

func TestLocalActionSourceSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeShellcheckFixture(t, outside, "action.yml", "name: Outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "source")); err != nil {
		t.Skip(err)
	}
	view := &LocalActionsCache{base: NewLocalActionsCache(&Project{root: root}, nil), sourceMappings: LocalActionSources{"./generated": "./source"}}
	if _, _, err := view.FindMetadata("./generated"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("got %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "action.yml"), filepath.Join(root, "manifest", "action.yml")); err != nil {
		t.Fatal(err)
	}
	view.sourceMappings["./generated"] = "./manifest"
	if _, _, err := view.FindMetadata("./generated"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalActionSourceValidation(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './source'}\n")
	writeShellcheckFixture(t, root, "source/action.yml", `name: source
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
	result := compositeAnalysis(t, root, "- uses: ./generated\n  id: generated\n  with:\n    unexpected: value\n- run: echo '${{ steps.generated.outputs.unknown }}'", AnalysisOptions{})
	for _, want := range []struct{ rule, message string }{{"action", "token"}, {"action", "unexpected"}, {"expression", "unknown"}} {
		if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == want.rule && strings.Contains(d.Message, want.message) }) {
			t.Fatalf("missing %s/%s: %+v", want.rule, want.message, result.Diagnostics)
		}
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
		return d.Rule == "action" && filepath.ToSlash(d.Path) == "source/action.yml" && d.Start.Line == 13
	}) {
		t.Fatalf("lost source diagnostic: %+v", result.Diagnostics)
	}
}

func TestLocalActionSourceCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "Source/action.yml", "name: Source\ndescription: Source\nruns: {using: composite, steps: []}\n")
	view := &LocalActionsCache{base: NewLocalActionsCache(&Project{root: root}, nil), sourceMappings: LocalActionSources{"./generated": "./source"}, caseInsensitive: true}
	if meta, _, err := view.FindMetadata("./GENERATED"); err != nil || meta == nil {
		t.Fatalf("%+v %v", meta, err)
	}
	view.sourceMappings["./Generated"] = "./Source"
	if _, _, err := view.FindMetadata("./GENERATED"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalActionSourceCustomReader(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
		t.Run(runner, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './virtual'}\n")
			manifest := filepath.Join(root, "virtual", "action.yml")
			reader := func(path string) ([]byte, error) {
				if path == manifest {
					return []byte("name: Virtual\ndescription: Virtual\noutputs:\n  result:\n    description: Result\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"), nil
				}
				return os.ReadFile(path)
			}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./generated\n        id: generated\n      - run: echo '${{ steps.generated.outputs.result }}'\n        shell: bash\n")
			config, err := ParseConfig([]byte("local-actions: {'./generated': './virtual'}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, ReadFile: reader, Sources: []SourceUnit{{Path: ".github/workflows/test.yml", Content: content, Config: config, Project: &Project{root: root}}}})
			if err != nil || len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, manifest) {
				t.Fatalf("direct analysis: %+v, %v", result, err)
			}
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", string(content))
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ReadFile: reader})
			if err != nil {
				t.Fatal(err)
			}
			result, err = session.Files([]string{workflow}, nil)
			if err != nil || len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, manifest) {
				t.Fatalf("session analysis: %+v, %v", result, err)
			}
		})
	}
}

func TestLocalActionSourceFailureReportedOncePerReference(t *testing.T) {
	for _, failure := range []string{"missing", "directory-symlink", "manifest-symlink"} {
		t.Run(failure, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "local-actions: {'./generated': './source'}\n")
			if failure != "missing" {
				outside := t.TempDir()
				manifest := writeShellcheckFixture(t, outside, "action.yml", "name: Outside\n")
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
			result := compositeAnalysis(t, root, "- uses: ./generated\n  id: first\n- uses: ./generated\n- uses: ./generated\n  id: third", AnalysisOptions{})
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

func TestLocalActionUnknownCheckoutMissingMetadata(t *testing.T) {
	for _, folded := range []bool{false, true} {
		root := t.TempDir()
		writeShellcheckFixture(t, root, "present/action.yml", "name: present\ndescription: present\nruns: {using: composite, steps: []}\n")
		view := &LocalActionsCache{base: NewLocalActionsCache(&Project{root: root}, nil), caseInsensitive: folded}
		view.setCheckout(runDirectory{kind: directoryUnknown})
		for range 2 {
			if meta, _, err := view.FindMetadata("./private/action"); err != nil || meta != nil {
				t.Fatalf("unknown checkout: %+v %v", meta, err)
			}
		}
		if meta, _, err := view.FindMetadata("./present"); err != nil || meta == nil {
			t.Fatalf("lost best-effort metadata: %+v %v", meta, err)
		}
		if _, _, err := view.FindMetadata("$/private/action"); err == nil {
			t.Fatal("self repository missing metadata must be reported")
		}
		view.sourceMappings = LocalActionSources{"./private/action": "./absent-source"}
		if _, _, err := view.FindMetadata("./private/action"); err == nil {
			t.Fatal("explicit missing source must be reported")
		}
		view.sourceMappings = nil
		view.setCheckout(runDirectory{kind: directoryKnown})
		if _, _, err := view.FindMetadata("./private/action"); err == nil {
			t.Fatal("known checkout missing metadata must be reported")
		}
	}
}

func TestMissingLocalActionMappedSourceDiagnostic(t *testing.T) {
	for _, folded := range []bool{false, true} {
		root := t.TempDir()
		view := &LocalActionsCache{base: NewLocalActionsCache(&Project{root: root}, nil), sourceMappings: LocalActionSources{"./generated": "./absent-source"}, caseInsensitive: folded}
		_, _, err := view.FindMetadata("./generated")
		if _, missing := errors.AsType[unresolvedLocalActionError](err); !missing {
			t.Fatalf("folded=%v: expected missing-local-action: %v", folded, err)
		}
		want := `source mapping for local action "./generated": local action "./absent-source" has no action.yml or action.yaml; check the path and ensure the action is available when linting`
		if err.Error() != want {
			t.Fatalf("folded=%v: got %q; want %q", folded, err, want)
		}
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
			root, _ := executableFixture(t)
			result := compositeAnalysis(t, root, tc.checkout+"\n- uses: ./private-action", AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "missing-local-action" })
			if found != tc.missing {
				t.Fatalf("missing=%t, expected=%t: %+v", found, tc.missing, result.Diagnostics)
			}
		})
	}
}

func TestMissingLocalActionSourceSuppression(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
		for _, ignored := range []bool{false, true} {
			root, _ := executableFixture(t)
			config, err := ParseConfig([]byte("local-actions: {'./generated': './absent-source'}\n"))
			if err != nil {
				t.Fatal(err)
			}
			comment := ""
			if ignored {
				comment = " # actionlint:ignore missing-local-action"
			}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./generated" + comment + "\n")
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: ".github/workflows/test.yml", Content: content, Config: config, Project: &Project{root: root}}}})
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
	root, _ := executableFixture(t)
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
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: "+tc.with+"\n- uses: ./vendor/missing\n- run: ./vendor/bad.sh", AnalysisOptions{})
			for _, rule := range []string{"missing-local-action", "executable-bit"} {
				found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == rule })
				// The unknown local action invalidates executable state; test that rule independently.
				if rule == "executable-bit" {
					script := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: "+tc.with+"\n- run: ./vendor/bad.sh\n  shell: bash\n  working-directory: .", AnalysisOptions{})
					found = slices.ContainsFunc(script.Diagnostics, func(d Diagnostic) bool { return d.Rule == rule })
				}
				if found != tc.known {
					t.Fatalf("%s finding=%v, want=%v: %+v", rule, found, tc.known, result.Diagnostics)
				}
			}
		})
	}
}

func TestConstantEnvironmentInsertionCheckout(t *testing.T) {
	root, _ := executableFixture(t)
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
				result := compositeAnalysis(t, root, checkout+tcRule.step, AnalysisOptions{})
				found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == tcRule.rule })
				if found != tc.known {
					t.Fatalf("%s finding=%v, want=%v: %+v", tcRule.rule, found, tc.known, result.Diagnostics)
				}
			}
		})
	}
}
