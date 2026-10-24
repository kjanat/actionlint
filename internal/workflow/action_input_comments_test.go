package workflow

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestAliasedActionInputsKeepDeclarationCommentsAndAnchoredSpans(t *testing.T) {
	for _, action := range []string{"actions/github-script@v7", "docker://example/image:1"} {
		for _, inserted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inserted=%t", action, inserted), func(t *testing.T) {
				indent := "          "
				insertion := ""
				anchorLine := 8
				if inserted {
					insertion = indent + "${{ insert }}:\n"
					indent += "  "
					anchorLine++
				}
				content := fmt.Sprintf(`on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: %s
        with:
%s`, action, insertion)
				content += strings.Join([]string{
					indent + `anchor: &script "echo café" # anchor comment`,
					indent + "# alias leading",
					indent + "script: *script # alias trailing",
					indent + "plain: *script",
					"",
				}, "\n")
				workflow, errors := Parse([]byte(content))
				if len(errors) != 0 {
					t.Fatalf("parse errors: %+v", errors)
				}
				exec, ok := workflow.Jobs["build"].Steps[0].Exec.(*ExecAction)
				if !ok {
					t.Fatal("action step missing")
				}
				inputs := exec.Inputs
				if inserted {
					if len(exec.InputInsertions) != 1 {
						t.Fatalf("insertions=%+v", exec.InputInsertions)
					}
					inputs = exec.InputInsertions[0].Inputs
				}
				anchor, alias, plain := inputs["anchor"], inputs["script"], inputs["plain"]
				if anchor == nil || alias == nil || plain == nil || anchor.Source == nil || alias.Source == nil || plain.Source == nil {
					t.Fatalf("missing script input sources: %+v", inputs)
				}
				want := []ScalarComment{
					{Text: "# alias leading", Pos: Pos{anchorLine + 1, len(indent) + 1}},
					{Text: "# alias trailing", Pos: Pos{anchorLine + 2, len(indent) + 17}, Trailing: true},
				}
				if !slices.Equal(alias.Source.Comments, want) {
					t.Fatalf("alias comments=%+v, want %+v", alias.Source.Comments, want)
				}
				if len(plain.Source.Comments) != 0 {
					t.Fatalf("unannotated alias inherited comments: %+v", plain.Source.Comments)
				}
				if len(anchor.Source.Comments) != 1 || anchor.Source.Comments[0].Text != "# anchor comment" {
					t.Fatalf("anchor comments changed: %+v", anchor.Source.Comments)
				}
				for _, input := range []*Input{anchor, alias, plain} {
					pos, mapped := input.Source.Position(1, 1)
					if input.Value.Value != "echo café" || !mapped || *pos != (Pos{anchorLine, len(indent) + 18}) || !slices.Equal(input.Source.Spans, anchor.Source.Spans) {
						t.Fatalf("anchored text/spans changed: value=%+v source=%+v position=%+v", input.Value, input.Source, pos)
					}
				}
			})
		}
	}
}
