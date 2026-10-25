package shellcheck

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/source"
)

// scriptPreparer exposes the same shell interpretation, expression masking and
// source preparation used by the native adapter through the public WIT import.
func scriptPreparer(scripts []analyzer.Script) analyzer.ScriptPreparer {
	return func(ctx context.Context, reference source.Reference, override *string) (*analyzer.PreparedScript, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, script := range scripts {
			if script.Reference == reference {
				return preparePluginScript(script, override)
			}
		}
		return nil, fmt.Errorf("unknown script reference")
	}
}

func preparePluginScript(script analyzer.Script, override *string) (*analyzer.PreparedScript, error) {
	shell := shellcheckShell{name: script.Shell}
	if script.Runtime != nil && script.Runtime.ShellKnown {
		shell = shellcheckShell{name: script.Runtime.Shell, implicit: script.Runtime.ShellImplicit}
	}
	dialect, setup := shell.analysis()
	var comments []analyzer.SourceComment
	var prefix strings.Builder
	for _, comment := range script.Comments {
		text, ok := strings.CutPrefix(comment.Text, "#")
		if ok && strings.HasPrefix(strings.TrimLeft(text, " \t"), "shellcheck") {
			comments = append(comments, comment)
			prefix.WriteString(comment.Text)
			prefix.WriteByte('\n')
		}
	}
	header, _, _ := shellcheckHeader(script.Text)
	combined, _, selected := shellcheckHeader(prefix.String() + header)
	if dialect == "" && !selected {
		return nil, nil
	}
	inferred := dialect
	if selected {
		dialect = shellcheckHeaderDialect(combined)
	} else if override != nil {
		dialect = *override
	}
	if dialect != inferred {
		setup = ""
	}
	sanitized := sanitizeExpressionsInScript(script.Text)
	prepared := prepareShellcheckScript(sanitized, prefix.String(), setup)
	result := &analyzer.PreparedScript{Text: prepared.text, Sanitized: sanitized, Shell: shell.name, Startup: setup}
	if !selected {
		result.Dialect = &dialect
	}
	commentIndex := 0
	for index, line := range prepared.sourceLines {
		switch {
		case line > 0:
			result.Lines = append(result.Lines, analyzer.PreparedLine{Kind: analyzer.PreparedScriptLine, Line: line})
		case index+1 == prepared.startupLine:
			result.Lines = append(result.Lines, analyzer.PreparedLine{Kind: analyzer.PreparedStartupLine})
		case commentIndex < len(comments):
			result.Lines = append(result.Lines, analyzer.PreparedLine{Kind: analyzer.PreparedCommentLine, Position: comments[commentIndex].Start})
			commentIndex++
		default:
			result.Lines = append(result.Lines, analyzer.PreparedLine{Kind: analyzer.PreparedStartupLine})
		}
	}
	for _, code := range excludedChecks(nil) {
		number, err := strconv.ParseUint(strings.TrimPrefix(code, "SC"), 10, 32)
		if err != nil {
			return nil, err
		}
		result.Excluded = append(result.Excluded, uint32(number))
	}
	return result, nil
}
