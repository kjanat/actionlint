package parser

import (
	"strings"
	"testing"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
)

func TestQuotedFoldedScriptSource(t *testing.T) {
	for _, tc := range []struct{ name, input, token, rawToken string }{
		{"double", `run: "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"tagged-double", `run: !!str "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"anchored-double", `run: &script "echo \"hello\" $foo"`, "$foo", "$foo"},
		{"tag-anchor", `run: !!str &script 'echo $foo'`, "$foo", "$foo"},
		{"anchor-tag", `run: &script !!str echo $foo`, "$foo", "$foo"},
		{"verbatim-tag", `run: !<tag:syntax.org,2002:str> "echo $foo"`, "$foo", "$foo"},
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
			root, err := codec.Parse([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			node := root.Content[0].Content[1]
			p := NewSourceParser([]byte(tc.input))
			source := p.ScriptSource(node)
			if source == nil {
				t.Fatalf("no mapping for %q decoded as %q", tc.input, node.Value)
			}
			if source.Literal {
				t.Fatal("quoted/folded source must not permit literal fixes")
			}
			offset := strings.Index(node.Value, tc.token)
			if offset < 0 {
				t.Fatalf("token %q missing from decoded value %q", tc.token, node.Value)
			}
			prefix := node.Value[:offset]
			line := strings.Count(prefix, "\n") + 1
			col := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
			var want workflowlang.Pos
			for i, raw := range strings.Split(tc.input, "\n") {
				if offset := strings.Index(raw, tc.rawToken); offset >= 0 {
					want = workflowlang.Pos{Line: i + 1, Col: utf8.RuneCountInString(raw[:offset]) + 1}
					break
				}
			}
			if got, ok := source.Position(line, col); !ok || *got != want {
				t.Fatalf("position = %v, %v; want %v", got, ok, want)
			}
			if tc.name == "unicode-escape" || tc.name == "quote-escape" {
				width := 6
				if tc.name == "quote-escape" {
					width = 2
				}
				want.Col += width
				if got, ok := source.EndPos(line, col+1); !ok || *got != want {
					t.Fatalf("escape end = %v, %v; want %v", got, ok, want)
				}
			}
		})
	}
}

func TestStandaloneSourceReconstructionPreservesLineBreaks(t *testing.T) {
	for _, ending := range []string{"\n", "\r", "\r\n", "\u0085", "\u2028", "\u2029"} {
		t.Run(ending, func(t *testing.T) {
			source := "run: >" + ending + "  echo $FOO" + ending + "next: value" + ending
			document := sourcemap.NewDocument(source)
			if got := scalarDocumentSource(document); got != source {
				t.Fatalf("reconstructed source = %q, want original line breaks in %q", got, source)
			}
			root, err := codec.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p := NewSourceParser([]byte(source))
			if p.ScriptSource(root.Content[0].Content[1]) == nil {
				t.Fatalf("standalone source mapping unavailable for line break %q", ending)
			}
		})
	}
}

func TestQuotedScalarEOFPosition(t *testing.T) {
	for _, quote := range []string{"\"", "'"} {
		for _, suffix := range []string{"\n  ", "\n\n  ", "\n\n\n  ", "  "} {
			for _, newline := range []string{"\n", "\r\n"} {
				t.Run(quote+suffix+newline, func(t *testing.T) {
					content := "run: " + quote + "echo ${{ github.ref"
					input := strings.ReplaceAll(content+suffix+quote+"\n", "\n", newline)
					root, err := codec.Parse([]byte(input))
					if err != nil {
						t.Fatal(err)
					}
					node := root.Content[0].Content[1]
					p := NewSourceParser([]byte(input))
					source := p.ScriptSource(node)
					lines := strings.Split(node.Value, "\n")
					got, ok := source.EndPos(len(lines), utf8.RuneCountInString(lines[len(lines)-1])+1)
					want := workflowlang.Pos{Line: 1, Col: len(content) + 1}
					if suffix == "  " {
						want.Col += len(suffix)
					}
					if !ok || *got != want {
						t.Fatalf("EOF position = %v, %v; want %v", got, ok, want)
					}
					if suffix == "\n  " {
						if got, ok := source.Position(1, utf8.RuneCountInString(node.Value)); ok {
							t.Fatalf("synthesized space has a start position: %v", got)
						}
					}
				})
			}
		}
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
				root, err := codec.Parse([]byte(input))
				if err != nil {
					t.Fatal(err)
				}
				node := root.Content[0].Content[1]
				p := NewSourceParser([]byte(input))
				source := p.ScriptSource(node)
				lines := strings.Split(node.Value, "\n")
				got, ok := source.EndPos(len(lines), utf8.RuneCountInString(lines[len(lines)-1])+1)
				want := workflowlang.Pos{Line: 2, Col: len("  echo ${{ github.ref") + 1}
				if !ok || *got != want {
					t.Fatalf("EOF position = %v, %v; want %v", got, ok, want)
				}
			})
		}
	}
}

func TestQuotedEscapedLineBreakEOFPosition(t *testing.T) {
	for _, escape := range []string{`\r`, `\n`, `\r\n`, `\n\r`, `\x0d`, `\u000d`, `\U0000000d`, `\x0a`, `\u000a`} {
		for _, suffix := range []string{"", "\n  ", "\n\n  "} {
			t.Run(escape+suffix, func(t *testing.T) {
				content := `run: "echo ${{ github.ref` + escape
				input := content + suffix + "\"\n"
				root, err := codec.Parse([]byte(input))
				if err != nil {
					t.Fatal(err)
				}
				node := root.Content[0].Content[1]
				p := NewSourceParser([]byte(input))
				source := p.ScriptSource(node)
				lines := strings.Split(node.Value, "\n")
				got, ok := source.EndPos(len(lines), utf8.RuneCountInString(lines[len(lines)-1])+1)
				want := workflowlang.Pos{Line: 1, Col: len(content) + 1}
				if !ok || *got != want {
					t.Fatalf("EOF = %v, %v; want %v", got, ok, want)
				}
			})
		}
	}
}
