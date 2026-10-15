package engine

import (
	"context"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/internal/workflowcommands"
)

type deprecatedCommandsAnalyzer struct{}

func (deprecatedCommandsAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	scripts := batch.scripts
	var findings []*Error
	for _, script := range scripts {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		run := script.run
		for _, command := range workflowcommands.Deprecated(run.Run.Value) {
			position := func(offset int, exclusive bool) (*Pos, bool) {
				prefix := run.Run.Value[:offset]
				line := strings.Count(prefix, "\n") + 1
				column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
				if exclusive {
					return run.source.endPos(line, column)
				}
				return run.source.pos(line, column)
			}
			start, startOK := position(command.Start, false)
			end, endOK := position(command.End, true)
			if !startOK || !endOK {
				start, end = run.Run.Pos, nil
			}
			finding := errorfAt(start, "deprecated-commands", "workflow command %q was deprecated. use `%s` instead: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions", command.Command, command.Replacement)
			finding.endPosition = end
			if end != nil && end.Line == start.Line && end.Col > start.Col {
				finding.endColumn = end.Col - 1
			}
			finding.Filepath, finding.source, finding.ignoreOrigin = script.path, script.content, script.origin
			findings = append(findings, finding)
		}
	}
	return findings, nil
}
