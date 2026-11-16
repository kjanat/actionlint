package shell

import (
	"context"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
)

// NewShellScriptInputBuilder exposes the profile's shell interpretation, template masking and
// source preparation to plugins through the public WIT import. It prepares
// only scripts whose occurrence is unique among scripts.
func NewShellScriptInputBuilder(scripts []inputs.Script) inputs.ShellScriptInputBuilder {
	byOccurrence := make(map[invocation.ID]inputs.Script, len(scripts))
	duplicates := make(map[invocation.ID]struct{})
	for _, script := range scripts {
		id := script.Occurrence.ID
		if _, exists := byOccurrence[id]; exists {
			duplicates[id] = struct{}{}
			continue
		}
		byOccurrence[id] = script
	}
	return func(ctx context.Context, id invocation.ID, override *string) (*inputs.ShellScriptInput, error) {
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

// BuildShellScriptInput supplies masked source, attached comments and interpreter
// facts. The plugin selects the languages it supports and interprets its own
// directives. Invalid interpreter invocations return no input.
func BuildShellScriptInput(script inputs.Script, override *string) (*inputs.ShellScriptInput, error) {
	shell := Shell{Name: script.Shell, Registry: script.Interpreters, Platform: TargetArguments(script.Environment)}
	if script.Runtime != nil && script.Runtime.ShellKnown {
		shell = Shell{Name: script.Runtime.Shell, Implicit: script.Runtime.ShellImplicit, Registry: script.Interpreters, Platform: TargetArguments(script.Environment)}
	}
	if shell.Invocation().Status == commands.Invalid {
		return nil, nil
	}
	dialect, setup := shell.Analysis()
	var comments []facts.SourceComment
	var prefix strings.Builder
	for _, comment := range script.Comments {
		if !strings.HasPrefix(comment.Text, "#") || strings.ContainsAny(comment.Text, "\r\n") {
			return nil, errors.New("script annotations must be physical YAML comment lines")
		}
		comments = append(comments, comment)
		prefix.WriteString(comment.Text)
		prefix.WriteByte('\n')
	}
	if override != nil && *override != dialect {
		dialect, setup = *override, ""
	}
	setup, _ = EnvironmentStartup(setup, script.Environment)
	masked := mask.Expressions(script.Text)
	prepared := Prepare(masked, prefix.String(), setup)
	result := &inputs.ShellScriptInput{Text: prepared.Text(), MaskedText: masked, Shell: shell.Name, ShellSetup: setup}
	if dialect != "" {
		result.Dialect = &dialect
	}
	commentIndex := 0
	for index, line := range prepared.sourceLines {
		switch {
		case line > 0:
			result.Lines = append(result.Lines, inputs.ShellScriptLine{Kind: inputs.ShellScriptSourceLine, Line: line})
		case prepared.Line(index+1).Origin == preparation.LineStartup:
			result.Lines = append(result.Lines, inputs.ShellScriptLine{Kind: inputs.ShellScriptGeneratedLine})
		case commentIndex < len(comments):
			result.Lines = append(result.Lines, inputs.ShellScriptLine{Kind: inputs.ShellScriptCommentLine, Position: comments[commentIndex].Start})
			commentIndex++
		default:
			result.Lines = append(result.Lines, inputs.ShellScriptLine{Kind: inputs.ShellScriptGeneratedLine})
		}
	}
	return result, nil
}
