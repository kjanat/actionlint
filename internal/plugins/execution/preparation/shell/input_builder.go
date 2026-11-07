package shell

import (
	"context"
	"errors"
	"strconv"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
)

// NewShellScriptInputBuilder exposes the profile's shell interpretation, template masking and
// source preparation to plugins through the public WIT import. It prepares
// only scripts whose occurrence is unique among scripts.
func NewShellScriptInputBuilder(scripts []checkinputs.Script) checkinputs.ShellScriptInputBuilder {
	byOccurrence := make(map[checkinputs.OccurrenceID]checkinputs.Script, len(scripts))
	duplicates := make(map[checkinputs.OccurrenceID]struct{})
	for _, script := range scripts {
		id := script.Occurrence.ID
		if _, exists := byOccurrence[id]; exists {
			duplicates[id] = struct{}{}
			continue
		}
		byOccurrence[id] = script
	}
	return func(ctx context.Context, id checkinputs.OccurrenceID, override *string) (*checkinputs.ShellScriptInput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if id == "" {
			return nil, errors.New("empty script occurrence")
		}
		if _, duplicate := duplicates[id]; duplicate {
			return nil, errors.New("ambiguous script occurrence")
		}
		script, exists := byOccurrence[id]
		if !exists {
			return nil, errors.New("unknown script occurrence")
		}
		return BuildShellScriptInput(script, override)
	}
}

// BuildShellScriptInput prepares a run block for a plugin. Directive comments the
// workflow attaches to the step precede the script; override replaces an
// inferred dialect. It returns nil when the script is not a supported shell
// script and no directive selects one.
func BuildShellScriptInput(script checkinputs.Script, override *string) (*checkinputs.ShellScriptInput, error) {
	shell := Shell{Name: script.Shell, Registry: script.Interpreters, Platform: TargetArguments(script.Environment)}
	if script.Runtime != nil && script.Runtime.ShellKnown {
		shell = Shell{Name: script.Runtime.Shell, Implicit: script.Runtime.ShellImplicit, Registry: script.Interpreters, Platform: TargetArguments(script.Environment)}
	}
	if shell.Invocation().Status == commands.Invalid {
		return nil, nil
	}
	dialect, setup := shell.Analysis()
	var comments []workflowfacts.SourceComment
	var prefix strings.Builder
	for _, comment := range script.Comments {
		text, ok := strings.CutPrefix(comment.Text, "#")
		if ok && strings.HasPrefix(strings.TrimLeft(text, " \t"), "shellcheck") {
			comments = append(comments, comment)
			prefix.WriteString(comment.Text)
			prefix.WriteByte('\n')
		}
	}
	header, _, _ := Header(script.Text)
	combined, _, selected := Header(prefix.String() + header)
	if dialect == "" && !selected {
		return nil, nil
	}
	inferred := dialect
	if selected {
		dialect = HeaderDialect(combined)
	} else if override != nil {
		dialect = *override
	}
	if dialect != inferred {
		setup = ""
	}
	setup, environmentExcluded := environmentStartup(setup, script.Environment)
	masked := mask.Expressions(script.Text)
	prepared := Prepare(masked, prefix.String(), setup)
	result := &checkinputs.ShellScriptInput{Text: prepared.Text(), MaskedText: masked, Shell: shell.Name, ShellSetup: setup}
	if !selected {
		result.Dialect = &dialect
	}
	commentIndex := 0
	for index, line := range prepared.sourceLines {
		switch {
		case line > 0:
			result.Lines = append(result.Lines, checkinputs.ShellScriptLine{Kind: checkinputs.ShellScriptSourceLine, Line: line})
		case index+1 == prepared.startupLine:
			result.Lines = append(result.Lines, checkinputs.ShellScriptLine{Kind: checkinputs.ShellScriptGeneratedLine})
		case commentIndex < len(comments):
			result.Lines = append(result.Lines, checkinputs.ShellScriptLine{Kind: checkinputs.ShellScriptCommentLine, Position: comments[commentIndex].Start})
			commentIndex++
		default:
			result.Lines = append(result.Lines, checkinputs.ShellScriptLine{Kind: checkinputs.ShellScriptGeneratedLine})
		}
	}
	for _, code := range Excluded(environmentExcluded) {
		number, err := strconv.ParseUint(strings.TrimPrefix(code, "SC"), 10, 32)
		if err != nil {
			return nil, err
		}
		result.Excluded = append(result.Excluded, uint32(number))
	}
	return result, nil
}
