package shell

import (
	"context"
	"errors"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestPreparerSelectsExactOccurrence(t *testing.T) {
	reference := sourcepos.Reference{Document: "workflow.yml", Revision: "r", Node: "run"}
	bashID, shID := checkinputs.OccurrenceID(strings.Repeat("a", 64)), checkinputs.OccurrenceID(strings.Repeat("b", 64))
	scripts := []checkinputs.Script{
		{Reference: reference, Text: "echo same", Occurrence: checkinputs.ScriptOccurrence{ID: bashID}, Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}},
		{Reference: reference, Text: "echo same", Occurrence: checkinputs.ScriptOccurrence{ID: shID}, Runtime: &scriptmodel.Execution{Shell: "sh", ShellKnown: true}},
	}
	prepare := Preparer(scripts)
	bash, err := prepare(t.Context(), bashID, nil)
	if err != nil || bash == nil || bash.Shell != "bash" || bash.Startup != "set -e -o pipefail" {
		t.Fatalf("bash occurrence preparation: %+v, %v", bash, err)
	}
	sh, err := prepare(t.Context(), shID, nil)
	if err != nil || sh == nil || sh.Shell != "sh" || sh.Startup != "set -e" {
		t.Fatalf("sh occurrence preparation: %+v, %v", sh, err)
	}
}

func TestPreparerRejectsInvalidOccurrencesAndHonorsCancellation(t *testing.T) {
	const id checkinputs.OccurrenceID = "known"
	script := checkinputs.Script{Text: "echo hi", Occurrence: checkinputs.ScriptOccurrence{ID: id}, Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}
	prepare := Preparer([]checkinputs.Script{script})
	for name, occurrence := range map[string]checkinputs.OccurrenceID{"empty": "", "unknown": "foreign"} {
		t.Run(name, func(t *testing.T) {
			if _, err := prepare(t.Context(), occurrence, nil); err == nil {
				t.Fatalf("%s occurrence accepted", name)
			}
		})
	}
	duplicate := Preparer([]checkinputs.Script{script, script})
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
	script := checkinputs.Script{Text: "#!/bin/bash\necho ${{ inputs.value }}\n", Comments: []workflowfacts.SourceComment{{Text: "# shellcheck disable=SC2086", Start: sourcepos.Position{Line: 4, Column: 7}}}, Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := PrepareScript(script, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.HasPrefix(prepared.Text, "#!/bin/bash\n# shellcheck disable=SC2086\nset -e -o pipefail\n") || strings.Contains(prepared.Text, "${{") {
		t.Fatalf("source: %q", prepared.Text)
	}
	want := []checkinputs.PreparedLine{{Kind: checkinputs.PreparedScriptLine, Line: 1}, {Kind: checkinputs.PreparedCommentLine, Position: script.Comments[0].Start}, {Kind: checkinputs.PreparedStartupLine}, {Kind: checkinputs.PreparedScriptLine, Line: 2}}
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
	script := checkinputs.Script{Text: "# shellcheck shell=sh\necho hi\n", Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := PrepareScript(script, nil)
	if err != nil || prepared == nil || prepared.Dialect != nil || prepared.Startup != "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	script.Text = "echo hi"
	script.Runtime.Shell = "python"
	prepared, err = PrepareScript(script, nil)
	if err != nil || prepared != nil {
		t.Fatalf("unsupported shell: %+v %v", prepared, err)
	}
}

// A closing marker inside a template's string literal does not end the
// template, so no part of it reaches the plugin.
func TestPluginPreparationMasksWholeTemplates(t *testing.T) {
	script := checkinputs.Script{Text: "echo ${{ '}}' }}\n", Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := PrepareScript(script, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Sanitized != "echo ___________\n" || strings.ContainsAny(prepared.Text, "'}") {
		t.Fatalf("template partially exposed: %q / %q", prepared.Sanitized, prepared.Text)
	}
}
