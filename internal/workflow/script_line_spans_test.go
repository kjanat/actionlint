package workflow

import (
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
)

func TestScriptSourceLineSpansMatchLookup(t *testing.T) {
	for name, run := range map[string]string{
		"literal": "|\n          echo ::set-output name=x::1\n          echo done\n",
		"keep":    "|+\n          echo ::set-output name=x::1\n\n",
		"folded":  ">\n          echo ::set-output\n          name=x::1\n\n          echo done\n",
		"quoted":  "\"echo \\\"a\\\"\\n::set-output name=x::1\"",
		"plain":   "echo a\n          ::set-output name=x::1",
		"unicode": "|\n          echo 日本語 ::set-output name=x::1\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + run + "\n"
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			exec, ok := workflow.Jobs["test"].Steps[0].Exec.(*ExecRun)
			if !ok || exec.Source == nil {
				t.Fatal("run step lost its script source")
			}
			script := analyzer.Script{Text: exec.Run.Value}
			for _, span := range exec.Source.LineSpans() {
				script.Spans = append(script.Spans, analyzer.Span{
					Line: span.Line, Start: span.Start, End: span.End,
					Document:    diagnostic.Position{Line: span.YamlLine, Column: span.YamlColumn},
					DocumentEnd: diagnostic.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
				})
			}
			for lineIndex, text := range strings.Split(exec.Run.Value, "\n") {
				line := lineIndex + 1
				for column := 1; column <= utf8.RuneCountInString(text)+1; column++ {
					for _, exclusive := range []bool{false, true} {
						var want *Pos
						var wantOK bool
						if exclusive {
							want, wantOK = exec.Source.EndPos(line, column)
						} else {
							want, wantOK = exec.Source.Position(line, column)
						}
						got, ok := script.Position(line, column, exclusive)
						if ok != wantOK {
							t.Fatalf("line %d column %d exclusive=%v: mapped=%v, parser mapped=%v", line, column, exclusive, ok, wantOK)
						}
						if ok && (got.Line != want.Line || got.Column != want.Col) {
							t.Fatalf("line %d column %d exclusive=%v: %+v, parser %+v", line, column, exclusive, got, want)
						}
					}
				}
			}
		})
	}
}
