package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/yamlsource"
)

func applyDiagnosticFix(t *testing.T, input string, fix diagmodel.Fix) string {
	t.Helper()
	offset := func(pos diagmodel.Position) int {
		start := 0
		for line := 1; line < pos.Line; line++ {
			i := strings.IndexByte(input[start:], '\n')
			if i < 0 {
				t.Fatalf("edit line outside source: %+v", pos)
			}
			start += i + 1
		}
		line, _, _ := strings.Cut(input[start:], "\n")
		i, ok := yamlsource.ByteOffsetAtColumn(line, pos.Column)
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

func TestShellcheckStructuredDiagnostic(t *testing.T) {
	command := shellcheckForTest(t)
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo\té 🐚 $VALUE\n"
	result, err := runTestAnalysis(t.Context(), Request{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(workflow)}}, Tools: (adapters.Options{ShellCheck: command}).Registrations()})
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
	rechecked, err := runTestAnalysis(t.Context(), Request{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(changed)}}, Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if err != nil || len(rechecked.Diagnostics) != 0 {
		t.Fatalf("fixed workflow findings: %+v, %v", rechecked, err)
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	var decoded diagmodel.Diagnostic
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Code != diagnostic.Code || len(decoded.Fixes) != 1 {
		t.Fatalf("structured JSON: %s, %v", encoded, err)
	}
}

func TestShellcheckCompositeFixPath(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	input := `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        echo $VALUE
`
	path := writeShellcheckFixture(t, root, "local/action.yml", input)
	result := compositeAnalysis(t, root, "- uses: ./local", Request{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if len(result.Diagnostics) != 1 || len(result.Diagnostics[0].Fixes) != 1 {
		t.Fatalf("composite fixes missing: %+v", result.Diagnostics)
	}
	fix := result.Diagnostics[0].Fixes[0]
	for _, edit := range fix.Edits {
		if filepath.Join(root, edit.Path) != path {
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
	if checked := compositeAnalysis(t, root, "- uses: ./local", Request{Tools: (adapters.Options{ShellCheck: command}).Registrations()}); len(checked.Diagnostics) != 0 {
		t.Fatalf("fixed composite findings: %+v", checked.Diagnostics)
	}
}
