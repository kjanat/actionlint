package component

import (
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"github.com/kjanat/wacogo"
)

type wireSpan struct {
	line, start, end      int
	document, documentEnd sourcetext.Position
}

func liftWireSpan(t *testing.T, value wacogo.Val) wireSpan {
	t.Helper()
	record, err := liftRecord(value, "span")
	if err != nil {
		t.Fatal(err)
	}
	u32 := func(name string) int {
		number, ok := record.Field(name).(wacogo.ValU32)
		if !ok {
			t.Fatalf("span %s is %T", name, record.Field(name))
		}
		return int(number)
	}
	span := wireSpan{line: u32("line"), start: u32("start"), end: u32("end")}
	if span.document, err = liftPosition(record.Field("document")); err != nil {
		t.Fatal(err)
	}
	option, ok := record.Field("document-end").(*wacogo.ValOption)
	if !ok {
		t.Fatalf("span document-end is %T", record.Field("document-end"))
	}
	if !option.IsNone() {
		if span.documentEnd, err = liftPosition(option.Val()); err != nil {
			t.Fatal(err)
		}
	}
	return span
}

// wirePosition maps a text (line, column) as plugins/protocol/wit/analyzer.wit
// tells guests to read the span records.
func wirePosition(spans []wireSpan, line, column int, exclusive bool) (sourcetext.Position, bool) {
	for _, span := range spans {
		if span.line != line {
			continue
		}
		inside := column >= span.start && column < span.end
		if exclusive {
			inside = column > span.start && column <= span.end || span.start == span.end && column == span.start
		}
		if !inside {
			continue
		}
		if span.documentEnd.Line != 0 {
			if exclusive && column == span.end {
				return span.documentEnd, true
			}
			return span.document, true
		}
		return sourcetext.Position{Line: span.document.Line, Column: span.document.Column + column - span.start}, true
	}
	return sourcetext.Position{}, false
}

func TestLoweredScriptSpansMatchSourceLookup(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]string{
		"literal": "|\n          echo ::set-output name=x::1\n          echo done\n",
		"keep":    "|+\n          echo ::set-output name=x::1\n\n",
		"folded":  ">\n          echo ::set-output\n          name=x::1\n\n          echo done\n",
		"quoted":  "\"echo \\\"a\\\"\\n::set-output name=x::1\"",
		"plain":   "echo a\n          ::set-output name=x::1",
		"unicode": "|\n          echo 日本語 ::set-output name=x::1\n",
		"escapes": "\"tab\\there\\u00e9\\n  next \\x41\\n\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + run + "\n"
			workflow, errs := parser.ParseWorkflow([]byte(document))
			if len(errs) != 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			exec, ok := workflow.Jobs["test"].Steps[0].Exec.(*definition.ExecRun)
			if !ok || exec.Source == nil {
				t.Fatal("run step lost its script source")
			}
			declaration := scripts.FromRun(exec)
			if len(declaration.Spans) == 0 {
				t.Fatal("declaration lost its source spans")
			}
			var spans []wireSpan
			for _, value := range lowerSpans(declaration.Text, declaration.Spans) {
				spans = append(spans, liftWireSpan(t, value))
			}
			for lineIndex, text := range strings.Split(declaration.Text, "\n") {
				line := lineIndex + 1
				for column := 1; column <= utf8.RuneCountInString(text)+1; column++ {
					for _, exclusive := range []bool{false, true} {
						lookup := exec.Source.Position
						if exclusive {
							lookup = exec.Source.EndPos
						}
						want, wantOK := lookup(line, column)
						for reader, mapped := range map[string]func() (sourcetext.Position, bool){
							"declaration": func() (sourcetext.Position, bool) { return declaration.Position(line, column, exclusive) },
							"wire":        func() (sourcetext.Position, bool) { return wirePosition(spans, line, column, exclusive) },
						} {
							got, ok := mapped()
							if ok != wantOK || ok && got != *want {
								t.Fatalf("%s line %d column %d exclusive=%v: %+v %v, source %+v %v", reader, line, column, exclusive, got, ok, want, wantOK)
							}
						}
					}
				}
			}
		})
	}
}
