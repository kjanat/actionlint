package actionlint

import (
	"encoding/json"
	"os/exec"

	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestCompositeActionRuleSelection(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout\n")
	for _, selection := range []string{"enabled", "removed", "empty"} {
		t.Run(selection, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: ./outer", AnalysisOptions{
				OnRulesCreated: func(rules []Rule) []Rule {
					switch selection {
					case "removed":
						return slices.DeleteFunc(rules, func(rule Rule) bool {
							_, action := rule.(*RuleAction)
							return action
						})
					case "empty":
						return nil
					default:
						return rules
					}
				},
			})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "action" })
			if found != (selection == "enabled") {
				t.Fatalf("action rule selection %q: %+v", selection, result.Diagnostics)
			}
		})
	}
}

func TestCompositeScriptNodeSharing(t *testing.T) {
	leaf := &yaml.Node{Kind: yaml.ScalarNode, Value: "hello", Line: 3, Column: 5}
	shared := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{leaf}}
	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: shared}
	root := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{shared, alias, alias}}
	clone, ok := compositeScriptNode(root, make(map[*yaml.Node]bool), make(map[*yaml.Node]*yaml.Node))
	if !ok || clone == root || len(clone.Content) != 3 {
		t.Fatalf("clone failed: %v, %+v", ok, clone)
	}
	if clone.Content[0] == shared || clone.Content[0] != clone.Content[1] || clone.Content[1] != clone.Content[2] {
		t.Fatal("repeated aliases must reuse the completed clone")
	}
	if got := clone.Content[0].Content[0]; got == leaf || got.Value != leaf.Value || got.Line != leaf.Line || got.Column != leaf.Column {
		t.Fatalf("source information lost: %+v", got)
	}
	if root.Content[1] != alias || alias.Kind != yaml.AliasNode || alias.Alias != shared {
		t.Fatal("shared metadata was mutated")
	}
}

func TestCompositeScriptNodeCycle(t *testing.T) {
	root := &yaml.Node{Kind: yaml.SequenceNode}
	root.Content = []*yaml.Node{{Kind: yaml.AliasNode, Alias: root}}
	done := make(map[*yaml.Node]*yaml.Node)
	if _, ok := compositeScriptNode(root, make(map[*yaml.Node]bool), done); ok {
		t.Fatal("accepted a cyclic node graph")
	}
	if _, ok := done[root]; ok {
		t.Fatal("cached an incomplete clone")
	}
}

func compositeAnalysis(t *testing.T, root, steps string, options AnalysisOptions) *AnalysisResult {
	t.Helper()
	workflow := writeShellcheckFixture(t, root, ".github/workflows/composite.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: python\n        working-directory: scripts\n    steps:\n      "+strings.ReplaceAll(steps, "\n", "\n      ")+"\n")
	options.WorkingDir = root
	session, err := NewAnalysisSession(options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompositeShellcheck(t *testing.T) {
	command := shellcheckForTest(t)
	for _, filename := range []string{"action.yml", "action.yaml"} {
		t.Run(filename, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
			metadata := writeShellcheckFixture(t, root, ".github/actions/lint/"+filename, "name: lint\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        echo $VALUE\n")
			result := compositeAnalysis(t, root, "- uses: ./.github/actions/lint\n- uses: ./.github/actions/lint", AnalysisOptions{Shellcheck: command})
			var findings []Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" {
					findings = append(findings, d)
				}
			}
			if len(findings) != 1 || findings[0].Path != metadata || findings[0].Start != (DiagnosticPosition{8, 14}) || !strings.Contains(findings[0].Message, "SC2086") {
				t.Fatalf("metadata diagnostic: %+v", result.Diagnostics)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("metadata not tracked: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckAcrossWorkflows(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: lint\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo $VALUE\n")
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n"
	first := writeShellcheckFixture(t, root, ".github/workflows/a.yml", workflow)
	second := writeShellcheckFixture(t, root, ".github/workflows/b.yml", workflow)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: shellcheckForTest(t)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{first, second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || result.Diagnostics[0].Path != metadata {
		t.Fatalf("expected one shared-action finding: %+v", result.Diagnostics)
	}
	if result.FileCount() != 2 || len(result.legacyErrors()) != 1 {
		t.Fatalf("file and finding counts: %d, %d", result.FileCount(), len(result.legacyErrors()))
	}
	for _, path := range []string{first, second, metadata} {
		if !slices.Contains(result.Inputs, path) {
			t.Errorf("input missing: %s", path)
		}
	}
	renderer, err := NewAnalysisRenderer("", "{{json .}}", false)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := renderer.Render(&out, result); err != nil {
		t.Fatal(err)
	}
	var findings []ErrorTemplateFields
	if err := json.Unmarshal([]byte(out.String()), &findings); err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Filepath != metadata || !strings.Contains(findings[0].Snippet, "echo $VALUE") {
		t.Fatalf("legacy rendering lost the shared source: %+v", findings)
	}
}

func TestCompositeShellcheckActionPaths(t *testing.T) {
	command := shellcheckForTest(t)
	t.Setenv("GITHUB_WORKSPACE", "")
	foreign := t.TempDir()
	t.Setenv("GITHUB_ACTION_PATH", foreign)
	writeShellcheckFixture(t, foreign, ".shellcheckrc", "disable=SC2086\n")
	writeShellcheckFixture(t, foreign, "lib/value.sh", "VALUE=42\n")
	for _, selection := range []string{
		`"${{ github.action_path }}/.shellcheckrc"`,
		`{source-path: ["${{ github.action_path }}/lib"]}`,
	} {
		t.Run(selection, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: "+selection+"}}\n")
			outerRC := writeShellcheckFixture(t, root, "outer/.shellcheckrc", "disable=SC2086\n")
			innerRC := writeShellcheckFixture(t, root, "inner/.shellcheckrc", "# Inner action keeps SC2086 enabled.\n")
			writeShellcheckFixture(t, root, "outer/lib/value.sh", "VALUE=42\n")
			writeShellcheckFixture(t, root, "inner/lib/value.sh", "VALUE='two words'\n")
			writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        . value.sh\n        echo $VALUE\n    - uses: ./inner\n")
			inner := writeShellcheckFixture(t, root, "inner/action.yaml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        . value.sh\n        echo $VALUE\n")
			result := compositeAnalysis(t, root, "- uses: ./outer\n- uses: ./outer", AnalysisOptions{Shellcheck: command})
			var findings []Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" {
					findings = append(findings, d)
				}
			}
			if len(findings) != 1 || findings[0].Path != inner || !strings.Contains(findings[0].Message, "SC2086") {
				t.Fatalf("action configuration leaked between invocations: %+v", findings)
			}
			if strings.Contains(selection, ".shellcheckrc") {
				for _, path := range []string{outerRC, innerRC} {
					if !slices.Contains(result.Inputs, path) {
						t.Errorf("action-specific rc file missing from inputs: %s", path)
					}
				}
			}
		})
	}
}

func TestCompositeShellcheckConfigValidation(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, actionSteps, workflowSteps string
		missingRC, wantError             bool
	}{
		{"python", "- shell: python\n  run: print('hello')", "- uses: ./local", false, false},
		{"python missing rc", "- shell: python\n  run: print('hello')", "- uses: ./local", true, true},
		{"powershell", "- shell: pwsh\n  run: Write-Output hello", "- uses: ./local", false, false},
		{"powershell missing rc", "- shell: pwsh\n  run: Write-Output hello", "- uses: ./local", true, true},
		{"uses only", "- uses: actions/checkout@v6", "- uses: ./local", false, false},
		{"uses only missing rc", "- uses: actions/checkout@v6", "- uses: ./local", true, true},
		{"no composite", "- uses: actions/checkout@v6", "- uses: actions/checkout@v6", false, true},
		{"workflow python", "- uses: actions/checkout@v6", "- uses: ./local\n- shell: python\n  run: print('hello')", false, true},
		{"workflow bash", "- uses: actions/checkout@v6", "- uses: ./local\n- shell: bash\n  run: echo hello", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '${{ github.action_path }}/.shellcheckrc'}}\n")
			writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(tc.actionSteps, "\n", "\n    ")+"\n")
			if !tc.missingRC {
				writeShellcheckFixture(t, root, "local/.shellcheckrc", "disable=SC2086\n")
			}
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      "+strings.ReplaceAll(tc.workflowSteps, "\n", "\n      ")+"\n")
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "tools.shellcheck.config") {
					t.Fatalf("expected config error, got %v", err)
				}
			} else if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("valid action context rejected: %v, %+v", err, result)
			}
		})
	}
}

func TestCompositeSyntaxAndNestedActions(t *testing.T) {
	command := shellcheckForTest(t)
	python, err := exec.LookPath("pyflakes")
	if err != nil {
		t.Skip("Pyflakes required")
	}
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	metadata := writeShellcheckFixture(t, root, "inner/action.yaml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        if then\n    - shell: python\n      run: |\n        print(\n    - uses: ./outer\n")
	result := compositeAnalysis(t, root, "- uses: ./outer", AnalysisOptions{Shellcheck: command, Pyflakes: python})
	for _, rule := range []string{"shellcheck", "pyflakes"} {
		if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == rule && d.Path == metadata }) {
			t.Errorf("missing %s syntax finding: %+v", rule, result.Diagnostics)
		}
	}
	if !slices.Contains(result.Inputs, metadata) {
		t.Fatalf("nested metadata not tracked: %v", result.Inputs)
	}
}

func TestCompositeExecutableBit(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ./bad.sh\n")
	for _, tc := range []struct {
		name, steps string
		want        bool
	}{
		{"checkout", "- uses: actions/checkout@v6\n- uses: ./local", true},
		{"true call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: true", true},
		{"true expression call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: ${{ true }}", true},
		{"skipped call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: false", false},
		{"skipped expression call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: ${{ false }}", false},
		{"conditional call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: github.event_name == 'push'", false},
		{"no checkout", "- uses: ./local", false},
		{"earlier chmod", "- uses: actions/checkout@v6\n- run: chmod +x bad.sh\n  shell: bash\n  working-directory: ''\n- uses: ./local", false},
		{"startup env", "- uses: actions/checkout@v6\n- uses: ./local\n  env:\n    BASH_ENV: setup.sh", false},
		{"caller PATH", "- uses: actions/checkout@v6\n- uses: ./local\n  env: {PATH: /usr/bin}", true},
		{"caller PATH and startup script", "- uses: actions/checkout@v6\n- uses: ./local\n  env: {PATH: /usr/bin, BASH_ENV: setup.sh}", false},
		{"background", "- uses: actions/checkout@v6\n- uses: ./local\n  background: true", false},
		{"background false", "- uses: actions/checkout@v6\n- uses: ./local\n  background: false", true},
		{"background expression false", "- uses: actions/checkout@v6\n- uses: ./local\n  background: ${{ false }}", true},
		{"background expression true", "- uses: actions/checkout@v6\n- uses: ./local\n  background: ${{ true }}", false},
		{"background expression unknown", "- uses: actions/checkout@v6\n- uses: ./local\n  background: ${{ inputs.background }}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.steps, AnalysisOptions{})
			found := false
			for _, d := range result.Diagnostics {
				if d.Rule == "executable-bit" {
					found = true
					if d.Path != metadata || !strings.Contains(d.Message, `script "bad.sh"`) {
						t.Fatalf("caller defaults or source leaked: %+v", d)
					}
				}
			}
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeMacOSExecutableBit(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ./SCRIPTS/BAD.SH\n")
	workflow := writeShellcheckFixture(t, root, ".github/workflows/mac.yml", "on: push\njobs:\n  test:\n    runs-on: macos-latest\n    steps:\n      - uses: actions/checkout@v6\n      - uses: ./local\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
		return d.Rule == "executable-bit" && d.Path == metadata && strings.Contains(d.Message, `script "scripts/bad.sh"`)
	}) {
		t.Fatalf("nested action lost macOS path matching: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedCheckoutGitEnvironment(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	for _, tc := range []struct {
		name, workflowEnv, jobEnv, callEnv, checkoutEnv string
		want                                            bool
	}{
		{"workflow", "env: {GIT_WORK_TREE: /tmp}\n", "", "", "", false},
		{"job", "", "    env: {GIT_WORK_TREE: /tmp}\n", "", "", false},
		{"caller", "", "", "        env: {GIT_WORK_TREE: /tmp}\n", "", false},
		{"checkout", "", "", "", "      env: {GIT_WORK_TREE: /tmp}\n", false},
		{"caller config", "", "", "        env: {GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: core.worktree, GIT_CONFIG_VALUE_0: /tmp}\n", "", false},
		{"caller trace", "", "", "        env: {GIT_TRACE: '1'}\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n"+tc.checkoutEnv+"    - shell: bash\n      run: ./bad.sh\n")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/env.yml", "on: push\n"+tc.workflowEnv+"jobs:\n  test:\n    runs-on: ubuntu-latest\n"+tc.jobEnv+"    steps:\n      - uses: ./local\n"+tc.callEnv)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested metadata checks lost: %v", result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestConditionalCompositeRetainsStaticChecks(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	inner := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        ./bad.sh\n        echo $VALUE\n")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- uses: ./local\n  if: false", AnalysisOptions{Shellcheck: command})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("conditional nested invocation must not claim execution: %+v", result.Diagnostics)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
		return d.Rule == "shellcheck" && d.Path == inner && strings.Contains(d.Message, "SC2086")
	}) {
		t.Fatalf("conditional nested script lost static checks: %+v", result.Diagnostics)
	}
}

func TestCompositeScriptConfig(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo $VALUE\n")
	for _, config := range []string{"tools: {shellcheck: false}\n", "paths:\n  'local/**':\n    ignore: ['SC2086']\n"} {
		writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
		result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
		if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" }) {
			t.Fatalf("config ignored: %+v", result.Diagnostics)
		}
	}
}

func TestCompositeShellcheckWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "scripts/lib.sh", "if then\n")
	writeShellcheckFixture(t, root, "lib.sh", "echo ok\n")
	for _, directory := range []string{"", "      working-directory: scripts\n"} {
		file := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n"+directory+"      run: . ./lib.sh\n")
		result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
		found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == file })
		if found != (directory != "") {
			t.Fatalf("directory %q: %+v", directory, result.Diagnostics)
		}
	}
}

func TestCompositeActionPathWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "lib.sh", "echo ok\n")
	writeShellcheckFixture(t, root, "outer/lib.sh", "echo ok\n")
	writeShellcheckFixture(t, root, "inner/lib.sh", "if then\n")
	inner := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: . ./lib.sh\n")
	outer := writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: . ./lib.sh\n")
	result := compositeAnalysis(t, root, "- uses: ./outer", AnalysisOptions{Shellcheck: command})
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == inner }) {
		t.Fatalf("missing sourced-file finding in nested action: %+v", result.Diagnostics)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == outer }) {
		t.Fatalf("nested action_path leaked into caller: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedMetadataErrors(t *testing.T) {
	root, _ := executableFixture(t)
	outer := writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	for _, content := range []string{"runs: [\n", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n"} {
		inner := writeShellcheckFixture(t, root, "inner/action.yml", content)
		result := compositeAnalysis(t, root, "- uses: ./outer\n- uses: ./inner", AnalysisOptions{})
		if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
			return d.Rule == "action" && (d.Path == outer && strings.Contains(d.Message, "could not parse") || d.Path == inner && strings.Contains(d.Message, "shell"))
		}) {
			t.Fatalf("nested metadata error lost: %+v", result.Diagnostics)
		}
	}
}

func TestCompositeExecutionState(t *testing.T) {
	root, _ := executableFixture(t)
	for _, tc := range []struct {
		name, actionSteps, workflowSteps string
		want                             bool
	}{
		{"numeric startup env", "- shell: bash\n  env: {BASH_ENV: 123}\n  run: ./bad.sh", "- uses: actions/checkout@v6\n- uses: ./local", false},
		{"caller PATH chmod uncertainty", "- shell: bash\n  run: chmod +x good.sh && ./bad.sh", "- uses: actions/checkout@v6\n- uses: ./local\n  env: {PATH: tools}", false},
		{"caller PATH builtin", "- shell: bash\n  run: echo ok; ./bad.sh", "- uses: actions/checkout@v6\n- uses: ./local\n  env: {PATH: /usr/bin}", true},
		{"checkout clean false", "- uses: actions/checkout@v6\n  with: {clean: false}\n- shell: bash\n  run: ./bad.sh", "- uses: ./local", false},
		{"conditional checkout", "- uses: actions/checkout@v6", "- uses: ./local\n  if: false\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"conditional checkout and invocation", "- uses: actions/checkout@v6\n- shell: bash\n  run: ./bad.sh", "- uses: ./local\n  if: false", false},
		{"checkout after conditional call", "- shell: bash\n  run: echo ok", "- uses: ./local\n  if: false\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
		{"skipped call preserves caller", "- shell: bash\n  run: chmod +x bad.sh", "- uses: actions/checkout@v6\n- uses: ./local\n  if: ${{ false }}\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
		{"contradictory call preserves caller", "- shell: bash\n  run: chmod +x bad.sh", "- uses: actions/checkout@v6\n- uses: ./local\n  if: success() && failure()\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", true},
		{"skipped background preserves caller", "- shell: bash\n  run: echo ok", "- uses: actions/checkout@v6\n- uses: ./local\n  if: false\n  background: true\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
		{"false background preserves caller", "- shell: bash\n  run: echo ok", "- uses: actions/checkout@v6\n- uses: ./local\n  background: ${{ false }}\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
		{"true composite checkout", "- uses: actions/checkout@v6", "- uses: ./local\n  if: ${{ true }}\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
		{"opaque composite preserves uncertainty", "- run: git config core.fileMode false && chmod +x bad.sh\n  shell: bash", "- uses: actions/checkout@v6\n- uses: ./local\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"conditional opaque composite preserves uncertainty", "- run: git config core.fileMode false && chmod +x bad.sh\n  shell: bash", "- uses: actions/checkout@v6\n- uses: ./local\n  if: github.event_name == 'push'\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"conditional background call", "- shell: bash\n  run: echo ok", "- uses: ./local\n  if: github.event_name == 'push'\n  background: true\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"conditional inner background", "- shell: bash\n  background: true\n  run: echo ok", "- uses: ./local\n  if: github.event_name == 'push'\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"inner chmod", "- shell: bash\n  run: chmod +x bad.sh", "- uses: actions/checkout@v6\n- uses: ./local\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
		{"read-only composite", "- shell: bash\n  run: echo ok", "- uses: actions/checkout@v6\n- uses: ./local\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(tc.actionSteps, "\n", "\n    ")+"\n")
			result := compositeAnalysis(t, root, tc.workflowSteps, AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeToleratedFailureState(t *testing.T) {
	root, _ := executableFixture(t)
	for _, setting := range []string{"true", "${{ github.event_name == 'push' }}", "false", "${{ false }}"} {
		t.Run(setting, func(t *testing.T) {
			metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n    - shell: bash\n      run: ./bad.sh\n")
			result := compositeAnalysis(t, root, "- uses: ./local\n  continue-on-error: "+setting+"\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", AnalysisOptions{})
			inner, outer := false, false
			for _, finding := range result.Diagnostics {
				if finding.Rule != "executable-bit" {
					continue
				}
				if finding.Path == metadata {
					inner = true
				} else {
					outer = true
				}
			}
			if !inner {
				t.Fatalf("current invocation lost finding: %+v", result.Diagnostics)
			}
			// A direct script call itself invalidates subsequent permissions. Use a
			// checkout-only body to isolate tolerated failure propagation below.
			if outer {
				t.Fatalf("script effects remained certain: %+v", result.Diagnostics)
			}
			writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n")
			result = compositeAnalysis(t, root, "- uses: ./local\n  continue-on-error: "+setting+"\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			want := setting == "false" || setting == "${{ false }}"
			if found != want {
				t.Fatalf("outgoing certainty = %v, want %v: %+v", found, want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutMetadataPaths(t *testing.T) {
	root, _ := executableFixture(t)
	outer := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ./source/bad.sh\n    - uses: ./source/inner\n")
	inner := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo missing shell\n")
	decoy := writeShellcheckFixture(t, root, "source/local/action.yml", "name: wrong metadata\nruns: {using: composite, steps: []}\n")
	for _, spec := range []string{"./source/local", "$/local"} {
		for _, removed := range []string{"", "executable-bit", "action"} {
			t.Run(spec+"/"+removed, func(t *testing.T) {
				result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: "+spec, AnalysisOptions{
					OnRulesCreated: func(rules []Rule) []Rule {
						return slices.DeleteFunc(rules, func(rule Rule) bool { return rule.Name() == removed })
					},
				})
				if !slices.Contains(result.Inputs, outer) || !slices.Contains(result.Inputs, inner) || slices.Contains(result.Inputs, decoy) {
					t.Fatalf("wrong metadata read: %v", result.Inputs)
				}
				for _, finding := range result.Diagnostics {
					if strings.Contains(finding.Message, "wrong metadata") {
						t.Fatalf("wrong metadata validated: %+v", finding)
					}
				}
				if removed != "executable-bit" && !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && d.Path == outer }) {
					t.Fatalf("missing composite script finding: %+v", result.Diagnostics)
				}
				if removed != "action" && !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
					return d.Rule == "action" && d.Path == inner && strings.Contains(d.Message, "shell")
				}) {
					t.Fatalf("nested metadata validation lost: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeCheckoutEmptyExpressions(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ./source/bad.sh\n")
	for _, inputs := range []string{
		"path: ' source '",
		"path: \"${{ ' source ' }}\"",
		"repository: \"${{ '' }}\"",
		"ref: \"${{ '' }}\"",
		"sparse-checkout: \"${{ '' }}\"",
		"repository: \"${{ '' }}\", ref: \"${{ '' }}\", sparse-checkout: \"${{ '' }}\"",
	} {
		t.Run(inputs, func(t *testing.T) {
			checkout := inputs
			if !strings.HasPrefix(checkout, "path:") {
				checkout = "path: source, " + checkout
			}
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {"+checkout+"}\n- uses: ./source/local", AnalysisOptions{})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("checkout metadata not resolved: %v", result.Inputs)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && d.Path == metadata }) {
				t.Fatalf("missing composite script finding: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutShellcheckWithoutExecutableBit(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: source\n      run: . ./lib.sh\n")
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	writeShellcheckFixture(t, root, "source/lib.sh", "echo wrong file\n")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: ./source/local", AnalysisOptions{
		Shellcheck: command,
		OnRulesCreated: func(rules []Rule) []Rule {
			return slices.DeleteFunc(rules, func(rule Rule) bool { return rule.Name() == "executable-bit" })
		},
	})
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == metadata }) {
		t.Fatalf("composite ShellCheck read wrong source: %+v", result.Diagnostics)
	}
}

func TestCompositeSparseCheckoutSources(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: . ./lib.sh\n    - shell: bash\n      run: echo $UNQUOTED\n")
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	for _, tc := range []struct {
		name, sparse string
		complete     bool
	}{
		{"sparse", "local", false},
		{"dynamic", "'${{ github.event.repository.name }}'", false},
		{"empty", "''", true},
		{"empty expression", "\"${{ '' }}\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {sparse-checkout: "+tc.sparse+"}\n- uses: ./local", AnalysisOptions{
				Shellcheck: command,
				OnRulesCreated: func(rules []Rule) []Rule {
					return slices.DeleteFunc(rules, func(rule Rule) bool { return rule.Name() == "executable-bit" })
				},
			})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("best-effort metadata validation lost: %v", result.Inputs)
			}
			inline, sourced := false, false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "shellcheck" || diagnostic.Path != metadata {
					continue
				}
				if strings.Contains(diagnostic.Message, "SC2086") {
					inline = true
				} else if strings.Contains(diagnostic.Message, "SC1094") {
					sourced = true
				}
			}
			if !inline || sourced != tc.complete {
				t.Fatalf("inline=%v, sourced=%v, complete=%v: %+v", inline, sourced, tc.complete, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutServerPaths(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	for _, tc := range []struct {
		name, server, spec string
		read               bool
	}{
		{"default server", "''", "./source/local", true},
		{"literal empty server", "\"${{ '' }}\"", "./source/local", true},
		{"alternate server", "https://git.example.com", "./source/local", false},
		{"unknown server", "'${{ inputs.server }}'", "./source/local", false},
		{"best effort metadata", "https://git.example.com", "./local", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: source, github-server-url: "+tc.server+"}\n- uses: "+tc.spec, AnalysisOptions{
				OnRulesCreated: func(rules []Rule) []Rule {
					return slices.DeleteFunc(rules, func(rule Rule) bool { return rule.Name() == "executable-bit" })
				},
			})
			if read := slices.Contains(result.Inputs, metadata); read != tc.read {
				t.Fatalf("metadata read = %v, want %v: %v", read, tc.read, result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckWindowsDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	for _, checkout := range []string{"", "source"} {
		t.Run(checkout, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
			prefix := ""
			if checkout != "" {
				prefix = checkout + "/"
			}
			writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./"+prefix+"inner\n")
			directory := strings.ReplaceAll(prefix+"scripts/build", "/", `\`)
			metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: '"+directory+"'\n      run: |\n        . ./lib.sh\n        echo $VALUE\n")
			writeShellcheckFixture(t, root, "scripts/build/lib.sh", "VALUE=42\n")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/composite.yml", "on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: actions/checkout@v6\n        with: {path: '"+checkout+"'}\n      - uses: ./"+prefix+"outer\n")
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested metadata not analyzed: %v", result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" }) {
				t.Fatalf("Windows composite source not resolved: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutActionOutputs(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\noutputs:\n  answer:\n    description: test\n    value: '42'\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	for _, spec := range []string{"./source/local", "$/local"} {
		t.Run(spec, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: "+spec+"\n  id: local\n- run: echo '${{ steps.local.outputs.missing }}'", AnalysisOptions{})
			if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "expression" && strings.Contains(d.Message, `property "missing" is not defined`)
			}) {
				t.Fatalf("action outputs were not resolved: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeParallelCheckoutPlacement(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo missing shell\n")
	for _, tc := range []struct {
		name, steps string
		read        bool
	}{
		{"sibling", "- parallel:\n    - uses: actions/checkout@v6\n      with: {path: source}\n    - uses: ./source/local", false},
		{"following step", "- parallel:\n    - uses: actions/checkout@v6\n      with: {path: source}\n- uses: ./source/local", false},
		{"later checkout", "- parallel:\n    - uses: actions/checkout@v6\n      with: {path: source}\n- uses: actions/checkout@v6\n  with: {path: source}\n- uses: ./source/local", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.steps, AnalysisOptions{})
			if read := slices.Contains(result.Inputs, metadata); read != tc.read {
				t.Fatalf("metadata read = %v, want %v: %v", read, tc.read, result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckRelocatedCheckout(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n      with: {path: relocated}\n    - shell: bash\n      working-directory: relocated\n      run: . ./lib.sh\n")
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	writeShellcheckFixture(t, root, "relocated/lib.sh", "echo wrong file\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == metadata }) {
		t.Fatalf("relocated checkout source was not checked: %+v", result.Diagnostics)
	}
}

func TestCompositeShellcheckCallerCheckoutState(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n      with: {path: relocated}\n")
	writeShellcheckFixture(t, root, "lib.sh", "VALUE=42\n")
	run := "- shell: bash\n  working-directory: .\n  run: |\n    . ./lib.sh\n    echo $VALUE"
	for _, tc := range []struct {
		name, steps string
		warnings    int
	}{
		{"relocated root", "- uses: ./local\n" + run, 1},
		{"relocated directory", "- uses: ./local\n" + strings.Replace(run, "working-directory: .", "working-directory: relocated", 1), 0},
		{"conditional call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: inputs.checkout\n" + run, 1},
		{"tolerated failure", "- uses: actions/checkout@v6\n- uses: ./local\n  continue-on-error: true\n" + run, 1},
		{"skipped call", "- uses: actions/checkout@v6\n- uses: ./local\n  if: false\n" + run, 0},
		{"retained root", "- uses: actions/checkout@v6\n- uses: ./local\n" + run, 0},
		{"parallel children and following step", "- uses: actions/checkout@v6\n- parallel:\n    " + strings.ReplaceAll(run, "\n", "\n    ") + "\n" + run, 2},
		{"checkout after parallel", "- uses: actions/checkout@v6\n- parallel:\n    - shell: bash\n      run: echo ok\n- uses: actions/checkout@v6\n" + run, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.steps, AnalysisOptions{Shellcheck: command})
			warnings := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "shellcheck" && strings.Contains(diagnostic.Message, "SC2086") {
					warnings++
				}
			}
			if warnings != tc.warnings {
				t.Fatalf("ShellCheck warnings=%d, want %d: %+v", warnings, tc.warnings, result.Diagnostics)
			}
		})
	}
}

func TestCompositeShellcheckCallerCheckoutAcrossJobs(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "setup/action.yml", "name: setup\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n")
	writeShellcheckFixture(t, root, "lib.sh", "VALUE=42\n")
	// Either traversal order leaves an unknown checkout before the other job.
	job := "    runs-on: ubuntu-latest\n    steps:\n      - uses: $/setup\n      - shell: bash\n        run: |\n          . ./lib.sh\n          echo $VALUE\n      - uses: actions/checkout@v6\n        with: {repository: other/repo}\n"
	workflow := writeShellcheckFixture(t, root, ".github/workflows/jobs.yml", "on: push\njobs:\n  first:\n"+job+"  second:\n"+job)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" }) {
		t.Fatalf("previous job checkout leaked into caller: %+v", result.Diagnostics)
	}
}

func TestCompositeCheckoutEmptyPath(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	for _, value := range []string{"", "${{ '' }}"} {
		t.Run(value, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: \""+value+"\"}\n- uses: ./local", AnalysisOptions{})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("empty checkout path did not use workspace root: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckUnknownCheckout(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n      with: {path: '${{ github.event.repository.name }}'}\n    - shell: bash\n      run: . ./lib.sh\n")
	writeShellcheckFixture(t, root, "lib.sh", "if then\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" }) {
		t.Fatalf("unknown checkout sourced local decoy: %+v", result.Diagnostics)
	}
}
