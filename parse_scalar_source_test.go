package actionlint

import (
	"strings"
	"testing"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

func TestQuotedFoldedScriptSource(t *testing.T) {
	for _, tc := range []struct{ name, input, token, rawToken string }{
		{"double", `run: "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"tagged-double", `run: !!str "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"anchored-double", `run: &script "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"tag-anchor", `run: !!str &script 'echo $foo'`, "$foo", "$foo"},
		{"anchor-tag", `run: &script !!str echo $foo`, "$foo", "$foo"},
		{"verbatim-tag", `run: !<tag:yaml.org,2002:str> "echo $foo"`, "$foo", "$foo"},
		{"multiline-properties", "run: &script # source anchor\n  !!str\n  \"echo $foo\"", "$foo", "$foo"},
		{"unicode-escape", `run: "echo \u0024foo"`, "$foo", `\u0024foo`},
		{"unicode-before", `run: "echo \u00e9 $foo"`, "$foo", "$foo"},
		{"newline-escape", `run: "echo\n$foo"`, "$foo", "$foo"},
		{"single", `run: 'echo ''hello'' $foo'`, "$foo", "$foo"},
		{"quote-escape", `run: 'echo ''hello'''`, "'hello", "''hello"},
		{"quoted-folding", "run: \"echo first\n  $foo\"", "$foo", "$foo"},
		{"quoted-blank", "run: 'echo first\n\n  $foo'", "$foo", "$foo"},
		{"quoted-continuation", "run: \"echo \\\n  $foo\"", "$foo", "$foo"},
		{"quoted-escaped-trailing-space", "run: \"echo\\ \n  $foo\"", "$foo", "$foo"},
		{"folded", "run: >\n  echo first\n  $foo\n", "$foo", "$foo"},
		{"tagged-folded", "run: !!str &script >\n  echo first\n  $foo\n", "$foo", "$foo"},
		{"folded-blank", "run: >-\n  echo first\n\n  $foo\n", "$foo", "$foo"},
		{"folded-indent", "run: >2-\n  echo first\n    $foo\n  done\n", "$foo", "$foo"},
		{"folded-tab", "run: >2-\n  echo first\n  \t$foo\n  done\n", "$foo", "$foo"},
		{"folded-leading-tab", "run: >2-\n  \t\n  $foo\n", "$foo", "$foo"},
		{"folded-leading", "run: >+\n\n  echo\n  $foo\n\n", "$foo", "$foo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.input), &root); err != nil {
				t.Fatal(err)
			}
			node := root.Content[0].Content[1]
			p := parser{sourceLines: splitSourceLines([]byte(tc.input))}
			source := p.scriptSource(node)
			if source == nil {
				t.Fatalf("no mapping for %q decoded as %q", tc.input, node.Value)
			}
			if source.literal {
				t.Fatal("quoted/folded source must not permit literal fixes")
			}
			offset := strings.Index(node.Value, tc.token)
			if offset < 0 {
				t.Fatalf("token %q missing from decoded value %q", tc.token, node.Value)
			}
			prefix := node.Value[:offset]
			line := strings.Count(prefix, "\n") + 1
			col := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
			var want Pos
			for i, raw := range strings.Split(tc.input, "\n") {
				if offset := strings.Index(raw, tc.rawToken); offset >= 0 {
					want = Pos{Line: i + 1, Col: utf8.RuneCountInString(raw[:offset]) + 1}
					break
				}
			}
			if got, ok := source.pos(line, col); !ok || *got != want {
				t.Fatalf("position = %v, %v; want %v", got, ok, want)
			}
			if tc.name == "unicode-escape" || tc.name == "quote-escape" {
				width := 6
				if tc.name == "quote-escape" {
					width = 2
				}
				want.Col += width
				if got, ok := source.endPos(line, col+1); !ok || *got != want {
					t.Fatalf("escape end = %v, %v; want %v", got, ok, want)
				}
			}
		})
	}
}

func TestBlockScalarEOFPosition(t *testing.T) {
	for _, header := range []string{"|", ">", "|-", ">-", "|+", ">+", "!!str &script |", "&script !!str >"} {
		for _, ending := range []string{"\n", "\r\n", ""} {
			t.Run(header+ending, func(t *testing.T) {
				input := "run: " + header + "\n  echo ${{ github.ref"
				if ending != "" {
					input = strings.ReplaceAll(input+"\nnext: value\n", "\n", ending)
				}
				var root yaml.Node
				if err := yaml.Unmarshal([]byte(input), &root); err != nil {
					t.Fatal(err)
				}
				node := root.Content[0].Content[1]
				p := parser{sourceLines: splitSourceLines([]byte(input))}
				source := p.scriptSource(node)
				lines := strings.Split(node.Value, "\n")
				got, ok := source.endPos(len(lines), utf8.RuneCountInString(lines[len(lines)-1])+1)
				want := Pos{Line: 2, Col: len("  echo ${{ github.ref") + 1}
				if !ok || *got != want {
					t.Fatalf("EOF position = %v, %v; want %v", got, ok, want)
				}
			})
		}
	}
}
