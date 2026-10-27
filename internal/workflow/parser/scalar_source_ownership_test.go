package parser

import (
	"reflect"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestScriptSourceResultsOwnTheirMutableState(t *testing.T) {
	const input = "script: &script echo $HI # anchor\nrun: *script # first\nnext: *script # second\n"
	root, err := codec.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cold", "warm", "alias", "nil-declaration"} {
		t.Run(mode, func(t *testing.T) {
			parser := NewSourceParser([]byte(input))
			parser.IndexScalarSources(root)
			projected := projectAliasTestInput(parser, root)
			entries := projected.Content[0].Content
			pristine := NewSourceParser([]byte(input))
			pristine.IndexScalarSources(root)
			get := func(p *Parser) *workflowlang.ScriptSource {
				switch mode {
				case "alias":
					return p.ScriptSourceForDeclaration(entries[3], PosAt(entries[2]))
				case "nil-declaration":
					return p.ScriptSourceForDeclaration(entries[1], nil)
				default:
					return p.ScriptSource(entries[1])
				}
			}
			if mode == "warm" {
				parser.ScriptSource(entries[1])
			}
			result, want := get(parser), get(pristine)
			peer := parser.ScriptSourceForDeclaration(entries[5], PosAt(entries[4]))
			wantPeer := pristine.ScriptSourceForDeclaration(entries[5], PosAt(entries[4]))
			if result == nil || len(result.Lines) == 0 || len(result.Spans) == 0 || len(result.Comments) == 0 {
				t.Fatal("fixture must expose every mutable source field")
			}
			result.ContentStart = workflowlang.Pos{Line: 99, Col: 99}
			result.ScalarEnd = workflowlang.Pos{Line: 99, Col: 99}
			result.Value = "changed"
			result.Literal = !result.Literal
			result.Lines[0].Start = 99
			result.Spans[0].YamlColumn = 99
			result.Comments[0].Text = "changed"
			if got := get(parser); !reflect.DeepEqual(got, want) {
				t.Fatalf("caller mutation corrupted the cached source: got %+v, want %+v", got, want)
			}
			if !reflect.DeepEqual(peer, wantPeer) {
				t.Fatalf("caller mutation crossed alias occurrences: got %+v, want %+v", peer, wantPeer)
			}
			if root.Content[0].Content[3].Kind != syntax.AliasNode {
				t.Fatal("source lookup changed the physical alias")
			}
		})
	}
}
