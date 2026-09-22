package actionlint

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func shellcheckFixSource(t *testing.T, input string) *scriptSource {
	t.Helper()
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(input), &root); err != nil {
		t.Fatal(err)
	}
	p := &parser{sourceLines: splitSourceLines([]byte(input))}
	return p.scriptSource(root.Content[0].Content[1])
}

func applyDiagnosticFix(t *testing.T, input string, fix DiagnosticFix) string {
	t.Helper()
	offset := func(pos DiagnosticPosition) int {
		start := 0
		for line := 1; line < pos.Line; line++ {
			i := strings.IndexByte(input[start:], '\n')
			if i < 0 {
				t.Fatalf("edit line outside source: %+v", pos)
			}
			start += i + 1
		}
		line, _, _ := strings.Cut(input[start:], "\n")
		i, ok := byteOffsetAtColumn(line, pos.Column)
		if !ok {
			t.Fatalf("edit column outside source: %+v", pos)
		}
		return start + i
	}
	result := input
	for _, edit := range slices.Backward(fix.Edits) {
		start, end := offset(edit.Start), offset(edit.End)
		result = result[:start] + edit.Replacement + result[end:]
	}
	return result
}

func TestShellcheckFixLiteralMapping(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run("newline="+newline, func(t *testing.T) {
			input := strings.ReplaceAll("run: |\n  #!/bin/bash\n  echo\té 🐚 $VALUE\n  echo $OTHER\nnext: unchanged\n", "\n", newline)
			source := shellcheckFixSource(t, input)
			script := prepareShellcheckScript(source.value, "# shellcheck disable=SC2154\n", "set -e -o pipefail")
			fix := &shellcheckFix{Replacements: []shellcheckReplacement{
				{Line: 4, EndLine: 4, Column: 10, EndColumn: 10, Replacement: `"`},
				{Line: 4, EndLine: 4, Column: 16, EndColumn: 16, Replacement: `"`},
				{Line: 5, EndLine: 5, Column: 6, EndColumn: 6, Replacement: `"`},
				{Line: 5, EndLine: 5, Column: 12, EndColumn: 12, Replacement: `"`},
			}}
			fixes := shellcheckDiagnosticFixes(source, script, fix, "Quote variables")
			if len(fixes) != 1 || len(fixes[0].Edits) != 4 {
				t.Fatalf("mapped fixes: %+v", fixes)
			}
			changed := applyDiagnosticFix(t, input, fixes[0])
			want := strings.ReplaceAll(strings.ReplaceAll(input, "$VALUE", `"$VALUE"`), "$OTHER", `"$OTHER"`)
			if changed != want {
				t.Fatalf("changed YAML: %q, want %q", changed, want)
			}
			var decoded struct {
				Run  string `yaml:"run"`
				Next string `yaml:"next"`
			}
			if err := yaml.Unmarshal([]byte(changed), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Run != "#!/bin/bash\necho\té 🐚 \"$VALUE\"\necho \"$OTHER\"\n" || decoded.Next != "unchanged" {
				t.Fatalf("decoded YAML changed unexpectedly: %+v", decoded)
			}
		})
	}
}

func TestShellcheckFixRejectsAmbiguousChanges(t *testing.T) {
	input := "run: |\n  echo $VALUE\n  echo $OTHER\n\n"
	for _, tc := range []struct {
		name         string
		input        string
		replacements []shellcheckReplacement
	}{
		{"generated config", input, []shellcheckReplacement{{Line: 1, EndLine: 1, Column: 1, EndColumn: 1, Replacement: "#"}}},
		{"generated startup", input, []shellcheckReplacement{{Line: 2, EndLine: 2, Column: 1, EndColumn: 1, Replacement: "#"}}},
		{"cross line", input, []shellcheckReplacement{{Line: 3, EndLine: 4, Column: 1, EndColumn: 3, Replacement: "echo"}}},
		{"newline insertion", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: "\nevil: key"}}},
		{"carriage return", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: "\r"}}},
		{"NUL", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: "\x00"}}},
		{"invalid UTF8", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: "\xff"}}},
		{"indent insertion", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 1, EndColumn: 1, Replacement: " "}}},
		{"erase content", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 1, EndColumn: 12, Replacement: ""}}},
		{"outside column", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 99, EndColumn: 99, Replacement: `"`}}},
		{"outside line", input, []shellcheckReplacement{{Line: 99, EndLine: 99, Column: 1, EndColumn: 1, Replacement: `"`}}},
		{"overlap", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 12, Replacement: "x"}, {Line: 3, EndLine: 3, Column: 8, EndColumn: 10, Replacement: "y"}}},
		{"shared insertion", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}, {Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: "{"}}},
		{"empty line", input, []shellcheckReplacement{{Line: 5, EndLine: 5, Column: 1, EndColumn: 1, Replacement: "echo"}}},
		{"plain", "run: echo $VALUE\n", []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"quoted", "run: 'echo $VALUE'\n", []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"folded", "run: >\n  echo $VALUE\n", []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"expression", "run: |\n  echo ${{ env.VALUE }} $OTHER\n", []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"partial fix", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}, {Line: 1, EndLine: 1, Column: 1, EndColumn: 1, Replacement: "#"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := shellcheckFixSource(t, tc.input)
			value := "echo $VALUE\n"
			if source != nil {
				value = source.value
			}
			script := prepareShellcheckScript(sanitizeExpressionsInScript(value), "# shellcheck disable=SC2154\n", "set -e")
			if fixes := shellcheckDiagnosticFixes(source, script, &shellcheckFix{tc.replacements}, "Fix"); len(fixes) != 0 {
				t.Fatalf("unsafe fix exposed: %+v", fixes)
			}
		})
	}
}

func TestShellcheckStructuredDiagnostic(t *testing.T) {
	command := shellcheckForTest(t)
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo\té 🐚 $VALUE\n"
	result, err := Analyze(t.Context(), AnalysisRequest{ShellCheck: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(workflow)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "SC2086" || diagnostic.Severity != "info" || len(diagnostic.Fixes) != 1 {
		t.Fatalf("ShellCheck metadata lost: %+v", diagnostic)
	}
	for _, edit := range diagnostic.Fixes[0].Edits {
		if edit.Path != "workflow.yml" {
			t.Fatalf("fix path lost: %+v", edit)
		}
	}
	changed := applyDiagnosticFix(t, workflow, diagnostic.Fixes[0])
	if changed != strings.Replace(workflow, "$VALUE", `"$VALUE"`, 1) {
		t.Fatalf("wrong ShellCheck edit: %q", changed)
	}
	rechecked, err := Analyze(t.Context(), AnalysisRequest{ShellCheck: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(changed)}}})
	if err != nil || len(rechecked.Diagnostics) != 0 {
		t.Fatalf("fixed workflow findings: %+v, %v", rechecked, err)
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Diagnostic
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Code != diagnostic.Code || len(decoded.Fixes) != 1 {
		t.Fatalf("structured JSON: %s, %v", encoded, err)
	}
}

func TestShellcheckCompositeFixPath(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	input := "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        echo $VALUE\n"
	path := writeShellcheckFixture(t, root, "local/action.yml", input)
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
	if len(result.Diagnostics) != 1 || len(result.Diagnostics[0].Fixes) != 1 {
		t.Fatalf("composite fixes missing: %+v", result.Diagnostics)
	}
	fix := result.Diagnostics[0].Fixes[0]
	for _, edit := range fix.Edits {
		if edit.Path != path {
			t.Fatalf("fix targeted workflow instead of composite: %+v", edit)
		}
	}
	changed := applyDiagnosticFix(t, input, fix)
	if changed != strings.Replace(input, "$VALUE", `"$VALUE"`, 1) {
		t.Fatalf("composite fix: %q", changed)
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if checked := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command}); len(checked.Diagnostics) != 0 {
		t.Fatalf("fixed composite findings: %+v", checked.Diagnostics)
	}
}
