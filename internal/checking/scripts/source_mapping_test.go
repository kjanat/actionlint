package scripts

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCanonicalScriptSourcesPreserveScalarMapping(t *testing.T) {
	t.Parallel()
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
				t.Parallel()
				text := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + fixture.run + "\n      - uses: actions/github-script@v7\n        with:\n          script: " + fixture.input + "\n"
				graph, collection, workflow := batchSourceFixtureWithWorkflow(t, []byte(strings.ReplaceAll(text, "\n", newline)))
				rawRun, rawInput := fixtureBodies(t, graph, collection, workflow)
				batch, err := NewBatch(graph, collection)
				if err != nil {
					t.Fatal(err)
				}
				units := batch.Scripts()
				assertScalarMapping(t, rawRun.Source, units[0].Run.Source)
				assertScalarMapping(t, rawInput.Source, units[1].Input.Value.Source)
				runPos, runHasPos := units[0].Run.Run.Position()
				inputPos, inputHasPos := units[1].Input.Value.Value.Position()
				if units[0].Run.Run.Text() != rawRun.Run.Value || units[1].Input.Value.Value.Text() != rawInput.Value.Value ||
					!runHasPos || runPos != *rawRun.Run.Pos || !inputHasPos || inputPos != *rawInput.Value.Pos {
					t.Fatal("canonical scalar changed decoded text or position")
				}
			})
		}
	}
}

func assertScalarMapping(t *testing.T, original, canonical *workflowlang.ScriptSource) {
	t.Helper()
	if original == nil || canonical != original {
		t.Fatal("unit lost its declared scalar source")
	}
}
