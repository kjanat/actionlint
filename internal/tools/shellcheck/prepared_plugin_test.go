package shellcheck

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestPluginPreparationPreservesCommentAndScriptCoordinates(t *testing.T) {
	script := analyzer.Script{Text: "#!/bin/bash\necho ${{ inputs.value }}\n", Runtime: &analyzer.Runtime{Shell: "bash", ShellKnown: true}, Comments: []analyzer.SourceComment{{Text: "# shellcheck disable=SC2086", Start: diagnostic.Position{Line: 4, Column: 7}}}}
	prepared, err := preparePluginScript(script, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.HasPrefix(prepared.Text, "#!/bin/bash\n# shellcheck disable=SC2086\nset -e -o pipefail\n") || strings.Contains(prepared.Text, "${{") {
		t.Fatalf("source: %q", prepared.Text)
	}
	want := []analyzer.PreparedLine{{Kind: analyzer.PreparedScriptLine, Line: 1}, {Kind: analyzer.PreparedCommentLine, Position: script.Comments[0].Start}, {Kind: analyzer.PreparedStartupLine}, {Kind: analyzer.PreparedScriptLine, Line: 2}}
	if len(prepared.Lines) != len(want) {
		t.Fatalf("lines: %+v", prepared.Lines)
	}
	for i, line := range want {
		if prepared.Lines[i] != line {
			t.Fatalf("line %d: %+v", i, prepared.Lines[i])
		}
	}
}

func TestPluginPreparationUsesDeclaredDialectWithoutRuntimeFlags(t *testing.T) {
	script := analyzer.Script{Text: "# shellcheck shell=sh\necho hi\n", Runtime: &analyzer.Runtime{Shell: "bash", ShellKnown: true}}
	prepared, err := preparePluginScript(script, nil)
	if err != nil || prepared == nil || prepared.Dialect != nil || prepared.Startup != "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	script.Text = "echo hi"
	script.Runtime.Shell = "python"
	prepared, err = preparePluginScript(script, nil)
	if err != nil || prepared != nil {
		t.Fatalf("unsupported shell: %+v %v", prepared, err)
	}
}
