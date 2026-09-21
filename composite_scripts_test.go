package actionlint

import (
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
		{"no checkout", "- uses: ./local", false},
		{"earlier chmod", "- uses: actions/checkout@v6\n- run: chmod +x bad.sh\n  shell: bash\n  working-directory: ''\n- uses: ./local", false},
		{"startup env", "- uses: actions/checkout@v6\n- uses: ./local\n  env:\n    BASH_ENV: setup.sh", false},
		{"background", "- uses: actions/checkout@v6\n- uses: ./local\n  background: true", false},
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
		{"checkout clean false", "- uses: actions/checkout@v6\n  with: {clean: false}\n- shell: bash\n  run: ./bad.sh", "- uses: ./local", false},
		{"conditional checkout", "- uses: actions/checkout@v6", "- uses: ./local\n  if: false\n- shell: bash\n  working-directory: ''\n  run: ./bad.sh", false},
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
