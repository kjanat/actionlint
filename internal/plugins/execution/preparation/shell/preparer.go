package shell

import (
	"context"
	"errors"
	"strconv"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
)

// Preparer exposes the profile's shell interpretation, template masking and
// source preparation to plugins through the public WIT import. It prepares
// only scripts whose occurrence is unique among scripts.
func Preparer(scripts []checkinputs.Script) checkinputs.ScriptPreparer {
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
	return func(ctx context.Context, id checkinputs.OccurrenceID, override *string) (*checkinputs.PreparedScript, error) {
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
		return PrepareScript(script, override)
	}
}

// PrepareScript prepares a run block for a plugin. Directive comments the
// workflow attaches to the step precede the script; override replaces an
// inferred dialect. It returns nil when the script is not a supported shell
// script and no directive selects one.
func PrepareScript(script checkinputs.Script, override *string) (*checkinputs.PreparedScript, error) {
	shell := Shell{Name: script.Shell}
	if script.Runtime != nil && script.Runtime.ShellKnown {
		shell = Shell{Name: script.Runtime.Shell, Implicit: script.Runtime.ShellImplicit}
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
	masked := mask.Expressions(script.Text)
	prepared := Prepare(masked, prefix.String(), setup)
	result := &checkinputs.PreparedScript{Text: prepared.Text(), Sanitized: masked, Shell: shell.Name, Startup: setup}
	if !selected {
		result.Dialect = &dialect
	}
	commentIndex := 0
	for index, line := range prepared.sourceLines {
		switch {
		case line > 0:
			result.Lines = append(result.Lines, checkinputs.PreparedLine{Kind: checkinputs.PreparedScriptLine, Line: line})
		case index+1 == prepared.startupLine:
			result.Lines = append(result.Lines, checkinputs.PreparedLine{Kind: checkinputs.PreparedStartupLine})
		case commentIndex < len(comments):
			result.Lines = append(result.Lines, checkinputs.PreparedLine{Kind: checkinputs.PreparedCommentLine, Position: comments[commentIndex].Start})
			commentIndex++
		default:
			result.Lines = append(result.Lines, checkinputs.PreparedLine{Kind: checkinputs.PreparedStartupLine})
		}
	}
	for _, code := range Excluded(nil) {
		number, err := strconv.ParseUint(strings.TrimPrefix(code, "SC"), 10, 32)
		if err != nil {
			return nil, err
		}
		result.Excluded = append(result.Excluded, uint32(number))
	}
	return result, nil
}
