package shellcheck

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func shellcheckFixSource(t *testing.T, input string) *workflowlang.ScriptSource {
	t.Helper()
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(input), &root); err != nil {
		t.Fatal(err)
	}
	p := workflowlang.NewSourceParser([]byte(input))
	return p.ScriptSource(root.Content[0].Content[1])
}

func TestShellcheckFixLiteralMapping(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run("newline="+newline, func(t *testing.T) {
			input := strings.ReplaceAll("run: |\n  #!/bin/bash\n  echo\té 🐚 $VALUE\n  echo $OTHER\nnext: unchanged\n", "\n", newline)
			source := shellcheckFixSource(t, input)
			script := prepareShellcheckScript(source.Value, "# shellcheck disable=SC2154\n", "set -e -o pipefail")
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
	input := `run: |
  echo $VALUE
  echo $OTHER

`
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
		{"folded", `run: >
  echo $VALUE
`, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"expression", `run: |
  echo ${{ env.VALUE }} $OTHER
`, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}}},
		{"partial fix", input, []shellcheckReplacement{{Line: 3, EndLine: 3, Column: 6, EndColumn: 6, Replacement: `"`}, {Line: 1, EndLine: 1, Column: 1, EndColumn: 1, Replacement: "#"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := shellcheckFixSource(t, tc.input)
			value := "echo $VALUE\n"
			if source != nil {
				value = source.Value
			}
			script := prepareShellcheckScript(sanitizeExpressionsInScript(value), "# shellcheck disable=SC2154\n", "set -e")
			if fixes := shellcheckDiagnosticFixes(source, script, &shellcheckFix{tc.replacements}, "Fix"); len(fixes) != 0 {
				t.Fatalf("unsafe fix exposed: %+v", fixes)
			}
		})
	}
}
