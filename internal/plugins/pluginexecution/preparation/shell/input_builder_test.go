package shell

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestPreparerSelectsExactOccurrence(t *testing.T) {
	t.Parallel()
	reference := sourcetext.Reference{Document: "workflow.yml", Revision: "r", Node: "run"}
	bashID, shID := invocation.ID(strings.Repeat("a", 64)), invocation.ID(strings.Repeat("b", 64))
	scripts := []checkinputs.Script{
		{Reference: reference, Text: "echo same", Occurrence: invocation.Script{ID: bashID}, Runtime: &scripts.Execution{Shell: "bash", ShellKnown: true}},
		{Reference: reference, Text: "echo same", Occurrence: invocation.Script{ID: shID}, Runtime: &scripts.Execution{Shell: "sh", ShellKnown: true}},
	}
	prepare := NewShellScriptInputBuilder(scripts)
	bash, err := prepare(t.Context(), bashID, nil)
	if err != nil || bash == nil || bash.Shell != "bash" || bash.ShellSetup != "set -e -o pipefail" {
		t.Fatalf("bash occurrence preparation: %+v, %v", bash, err)
	}
	sh, err := prepare(t.Context(), shID, nil)
	if err != nil || sh == nil || sh.Shell != "sh" || sh.ShellSetup != "set -e" {
		t.Fatalf("sh occurrence preparation: %+v, %v", sh, err)
	}
}

func TestPreparerRejectsInvalidOccurrencesAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	const id invocation.ID = "known"
	script := checkinputs.Script{Text: "echo hi", Occurrence: invocation.Script{ID: id}, Runtime: &scripts.Execution{Shell: "bash", ShellKnown: true}}
	prepare := NewShellScriptInputBuilder([]checkinputs.Script{script})
	for name, occurrence := range map[string]invocation.ID{"empty": "", "unknown": "foreign"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := prepare(t.Context(), occurrence, nil); err == nil {
				t.Fatalf("%s occurrence accepted", name)
			}
		})
	}
	duplicate := NewShellScriptInputBuilder([]checkinputs.Script{script, script})
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
	t.Parallel()
	script := checkinputs.Script{Text: "#!/bin/bash\necho ${{ inputs.value }}\n", Comments: []facts.SourceComment{{Text: "# shellcheck disable=SC2086", Start: sourcetext.Position{Line: 4, Column: 7}}}, Runtime: &scripts.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := BuildShellScriptInput(script, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.HasPrefix(prepared.Text, "#!/bin/bash\n# shellcheck disable=SC2086\nset -e -o pipefail\n") || strings.Contains(prepared.Text, "${{") {
		t.Fatalf("source: %q", prepared.Text)
	}
	want := []checkinputs.ShellScriptLine{{Kind: checkinputs.ShellScriptSourceLine, Line: 1}, {Kind: checkinputs.ShellScriptCommentLine, Position: script.Comments[0].Start}, {Kind: checkinputs.ShellScriptGeneratedLine}, {Kind: checkinputs.ShellScriptSourceLine, Line: 2}}
	if len(prepared.Lines) != len(want) {
		t.Fatalf("lines: %+v", prepared.Lines)
	}
	for i, line := range want {
		if prepared.Lines[i] != line {
			t.Fatalf("line %d: %+v", i, prepared.Lines[i])
		}
	}
}

func TestPluginPreparationReportsInterpreterWithoutInterpretingToolDirectives(t *testing.T) {
	t.Parallel()
	script := checkinputs.Script{Text: "# shellcheck shell=sh\necho hi\n", Runtime: &scripts.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := BuildShellScriptInput(script, nil)
	if err != nil || prepared == nil || prepared.Dialect == nil || *prepared.Dialect != "bash" || prepared.ShellSetup != "set -e -o pipefail" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	script.Runtime.Shell = "python"
	prepared, err = BuildShellScriptInput(script, nil)
	if err != nil || prepared == nil || prepared.Dialect != nil {
		t.Fatalf("unknown POSIX dialect lost its source: %+v %v", prepared, err)
	}
	if len(prepared.Excluded) != 0 {
		t.Fatal("the host supplied tool-specific exclusions")
	}
}

// A closing marker inside a template's string literal does not end the
// template, so no part of it reaches the plugin.
func TestPluginPreparationMasksWholeTemplates(t *testing.T) {
	t.Parallel()
	script := checkinputs.Script{Text: "echo ${{ '}}' }}\n", Runtime: &scripts.Execution{Shell: "bash", ShellKnown: true}}
	prepared, err := BuildShellScriptInput(script, nil)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.MaskedText != "echo ___________\n" || strings.ContainsAny(prepared.Text, "'}") {
		t.Fatalf("template partially exposed: %q / %q", prepared.MaskedText, prepared.Text)
	}
}
