package shellcheck

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
)

func TestScriptPreparerSelectsExactOccurrence(t *testing.T) {
	reference := source.Reference{Document: "workflow.yml", Revision: "r", Node: "run"}
	bashID, shID := analyzer.OccurrenceID(strings.Repeat("a", 64)), analyzer.OccurrenceID(strings.Repeat("b", 64))
	scripts := []analyzer.Script{
		{Reference: reference, Occurrence: analyzer.ScriptOccurrence{ID: bashID}, Text: "echo same", Runtime: &analyzer.Runtime{Shell: "bash", ShellKnown: true}},
		{Reference: reference, Occurrence: analyzer.ScriptOccurrence{ID: shID}, Text: "echo same", Runtime: &analyzer.Runtime{Shell: "sh", ShellKnown: true}},
	}
	prepare := scriptPreparer(scripts)
	bash, err := prepare(t.Context(), bashID, nil)
	if err != nil || bash == nil || bash.Shell != "bash" || bash.Startup != "set -e -o pipefail" {
		t.Fatalf("bash occurrence preparation: %+v, %v", bash, err)
	}
	sh, err := prepare(t.Context(), shID, nil)
	if err != nil || sh == nil || sh.Shell != "sh" || sh.Startup != "set -e" {
		t.Fatalf("sh occurrence preparation: %+v, %v", sh, err)
	}
}

func TestScriptPreparerRejectsInvalidOccurrencesAndHonorsCancellation(t *testing.T) {
	const id analyzer.OccurrenceID = "known"
	script := analyzer.Script{Occurrence: analyzer.ScriptOccurrence{ID: id}, Text: "echo hi", Runtime: &analyzer.Runtime{Shell: "bash", ShellKnown: true}}
	prepare := scriptPreparer([]analyzer.Script{script})
	for name, occurrence := range map[string]analyzer.OccurrenceID{"empty": "", "unknown": "foreign"} {
		t.Run(name, func(t *testing.T) {
			if _, err := prepare(t.Context(), occurrence, nil); err == nil {
				t.Fatalf("%s occurrence accepted", name)
			}
		})
	}
	duplicate := scriptPreparer([]analyzer.Script{script, script})
	if _, err := duplicate(t.Context(), id, nil); err == nil {
		t.Fatal("ambiguous duplicate occurrence accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepare(canceled, id, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preparation error: %v", err)
	}
}

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
