package workflow

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestScalarAttachedComments(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []ScalarComment
	}{
		{"leading", "# shellcheck disable=SC2086\nrun: echo $HI\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 1}}}},
		{"trailing", "run: echo $HI # shellcheck disable=SC2086\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 15}, Trailing: true}}},
		{"block-header", "run: | # shellcheck disable=SC2086\n  echo $HI\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 8}, Trailing: true}}},
		{"body", "run: |\n  # shellcheck disable=SC2086\n  echo $HI\n", nil},
		{"quoted", "run: 'echo $HI # shellcheck disable=SC2086'\n", nil},
		{"quoted-trailing", "run: 'echo $HI # comment' # shellcheck disable=SC2086\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 27}, Trailing: true}}},
		{"multiline-quoted-trailing", "run: 'echo\n  # shellcheck disable=SC2086\n  end' # shellcheck disable=SC2086\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{3, 8}, Trailing: true}}},
		{"unicode-trailing", "run: echo café # shellcheck disable=SC2086\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 16}, Trailing: true}}},
		{"indented-leading", "  # shellcheck disable=SC2086\n  run: echo $HI\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 3}}}},
		{"matching-block-body", "run: | # shellcheck disable=SC2086\n  # shellcheck disable=SC2086\n  echo $HI\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{1, 8}, Trailing: true}}},
		{"following-comment", "run: echo $HI\n# shellcheck disable=SC2086\n", nil},
		{"blank-separator", "# shellcheck disable=SC2086\n\nrun: echo $HI\n", nil},
		{"preceding-entry", "name: example\n# shellcheck disable=SC2086\nrun: echo $HI\n", []ScalarComment{{Text: "# shellcheck disable=SC2086", Pos: Pos{2, 1}}}},
		{"group", "# ordinary comment\n# shellcheck disable=SC2086\nrun: echo $HI\n", []ScalarComment{{Text: "# ordinary comment", Pos: Pos{1, 1}}, {Text: "# shellcheck disable=SC2086", Pos: Pos{2, 1}}}},
	} {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(tc.name+newline, func(t *testing.T) {
				input := strings.ReplaceAll(tc.input, "\n", newline)
				var root yaml.Node
				if err := yaml.Unmarshal([]byte(input), &root); err != nil {
					t.Fatal(err)
				}
				mapping := root.Content[0]
				node := mapping.Content[len(mapping.Content)-1]
				parser := NewSourceParser([]byte(input))
				parser.IndexScalarSources(&root)
				source := parser.ScriptSource(node)
				if source == nil {
					t.Fatal("missing scalar source")
				}
				if len(source.Comments) != len(tc.want) {
					t.Fatalf("comments=%+v, want %+v", source.Comments, tc.want)
				}
				for i, comment := range source.Comments {
					if comment != tc.want[i] {
						t.Fatalf("comment=%+v, want %+v", comment, tc.want[i])
					}
				}
			})
		}
	}
}

func TestAliasDeclarationCommentsKeepAnchoredScriptSpans(t *testing.T) {
	const input = "script: &script echo $HI # shellcheck disable=SC2034\n# shellcheck disable=SC2086\nrun: *script\nnext: *script # shellcheck shell=sh\n"
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(input), &root); err != nil {
		t.Fatal(err)
	}
	parser := NewSourceParser([]byte(input))
	parser.IndexScalarSources(&root)
	parser.resolveAliases(&root)
	entries := root.Content[0].Content
	for _, tc := range []struct {
		index int
		want  ScalarComment
	}{
		{2, ScalarComment{Text: "# shellcheck disable=SC2086", Pos: Pos{2, 1}}},
		{4, ScalarComment{Text: "# shellcheck shell=sh", Pos: Pos{4, 15}, Trailing: true}},
	} {
		source := parser.ScriptSourceForDeclaration(entries[tc.index+1], PosAt(entries[tc.index]))
		if source == nil || len(source.Comments) != 1 || source.Comments[0] != tc.want {
			t.Fatalf("alias source=%+v, want %+v", source, tc.want)
		}
		if source.Value != "echo $HI" {
			t.Fatalf("alias text changed: %q", source.Value)
		}
		pos, ok := source.Position(1, 6)
		if !ok || *pos != (Pos{1, 22}) {
			t.Fatalf("alias token position=%+v, mapped=%v", pos, ok)
		}
	}
	anchor := parser.ScriptSource(entries[1])
	if len(anchor.Comments) != 1 || anchor.Comments[0].Text != "# shellcheck disable=SC2034" {
		t.Fatalf("alias comments leaked into anchor: %+v", anchor.Comments)
	}
}
