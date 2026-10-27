package execution

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestCanonicalScriptSourcesPreserveScalarMapping(t *testing.T) {
	for _, fixture := range []struct{ name, run, input string }{
		{"plain", "echo é 🐚", "echo é 🐚"},
		{"single quoted", "'echo ''é'' 🐚'", "'echo ''é'' 🐚'"},
		{"double quoted", `"echo é\nnext"`, `"echo é\nnext"`},
		{"literal", "| # run header\n          echo é\t🐚\n          echo next", "| # input header\n            echo é\t🐚\n            echo next"},
		{"folded", ">-\n          echo é\n          🐚", ">-\n            echo é\n            🐚"},
		{"empty input", "echo ok", "''"},
		{"expression", "'${{ env.SCRIPT }}'", "'${{ env.SCRIPT }}'"},
		{"alias", "&script |\n          echo é\t🐚\n          echo next", "*script"},
	} {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(fixture.name+"/"+map[string]string{"\n": "LF", "\r\n": "CRLF"}[newline], func(t *testing.T) {
				text := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + fixture.run + "\n      - uses: actions/github-script@v7\n        with:\n          script: " + fixture.input + "\n"
				graph, collection := batchSourceFixture(t, []byte(strings.ReplaceAll(text, "\n", newline)))
				rawRun, rawInput := fixtureBodies(t, graph, collection)
				batch, err := NewBatch(graph, collection)
				if err != nil {
					t.Fatal(err)
				}
				units := batch.Scripts()
				assertScalarMapping(t, rawRun.Source, units[0].Run.Source())
				assertScalarMapping(t, rawInput.Source, units[1].Input.Value.Source())
				runPos, runHasPos := units[0].Run.Run().Pos()
				inputPos, inputHasPos := units[1].Input.Value.Value().Pos()
				if units[0].Run.Run().Value() != rawRun.Run.Value || units[1].Input.Value.Value().Value() != rawInput.Value.Value ||
					!runHasPos || runPos != *rawRun.Run.Pos || !inputHasPos || inputPos != *rawInput.Value.Pos {
					t.Fatal("canonical scalar changed decoded text or position")
				}
			})
		}
	}
}

func assertScalarMapping(t *testing.T, original *workflowlang.ScriptSource, canonical documentparse.ScriptSourceView) {
	t.Helper()
	if original == nil || !canonical.Present() {
		t.Fatal("scalar source absent")
	}
	if canonical.Value() != original.Value || canonical.Literal() != original.Literal || canonical.ContentStart() != original.ContentStart || canonical.ScalarEnd() != original.ScalarEnd ||
		!reflect.DeepEqual(slices.Collect(canonical.Spans()), original.Spans) || !reflect.DeepEqual(slices.Collect(canonical.Comments()), original.Comments) ||
		!reflect.DeepEqual(slices.Collect(canonical.LineSpans()), original.LineSpans()) || canonical.LinesLen() != len(original.Lines) {
		t.Fatal("canonical source changed scalar metadata, spans, or comments")
	}
	for index, originalLine := range original.Lines {
		line, ok := canonical.Line(index)
		if !ok || line.Start != originalLine.Start || line.End != originalLine.End {
			t.Fatal("canonical source changed line bounds")
		}
	}
	for line := 0; line <= len(original.Lines)+1; line++ {
		for column := range 49 {
			position, ok := original.Position(line, column)
			actual, actualOK := canonical.Position(line, column)
			if ok != actualOK || ok && *position != actual {
				t.Fatalf("Position(%d,%d) changed: %v/%v != %v/%v", line, column, position, ok, actual, actualOK)
			}
			end, endOK := original.EndPos(line, column)
			actualEnd, actualEndOK := canonical.EndPos(line, column)
			if endOK != actualEndOK || endOK && *end != actualEnd {
				t.Fatalf("EndPos(%d,%d) changed: %v/%v != %v/%v", line, column, end, endOK, actualEnd, actualEndOK)
			}
		}
	}
}
