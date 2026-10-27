package workflow_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/source/text"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

var scalarDecoderInvariantCorpus = []string{
	"run: echo hello\n",
	"run: echo first\n  second\nnext: done\n",
	"run: echo hello # comment\n",
	"run: 'echo ''quoted'' text'\n",
	`run: "echo \"quoted\" \\ slash"`,
	`run: "\0\a\b\t\n\v\f\r\e\ \"\\\N\_\L\P"`,
	`run: "\x41\u00e9\U0001F600"`,
	`"\'00000"`,
	"run: \"escaped\\\ttab\"\n",
	"run: \"first\n  second\"\n",
	"run: 'first\n\n  second'\n",
	"run: \"first\\\n  second\"\n",
	"run: \"first\\\n\n  second\"\n",
	"run: \"first\\ \n  second\"\n",
	"run: |\n  first\n  second\n",
	"run: |-\n  first\n  second\n\n",
	"run: |+\n\n  first\n\n",
	"run: >\n  first\n  second\n",
	"run: >-\n  first\n\n  second\n",
	"run: >+\n\n  first\n\n",
	"run: >2-\n  first\n    indented\n  last\n",
	"run: |2+\n    first\n  second\n\n",
	"run: >2-\n  \tindented\n  last\n",
	"- |2-\n  first\n- >2\n  second\n",
	"run: !!str &script \"echo \\u0024HOME\"\ncopy: *script\n",
	"run: &script # comment\n  !!str\n  'echo hello'\n",
	"run: !<tag:syntax.org,2002:str> hello\n",
	"{plain: hello, 'quoted:key': [one, \"two\"], last: end}\n",
	"{a: {b: c}, d: [e, f]}\n",
	"? multi\n  line\n: value\n",
	"'a:b': café 日本 😀\n",
	"empty: ''\nother: \"\"\nnull:\nblock: |\n",
	"run: |\n  a\r\n  b\r\nnext: done\r\n",
	"run: \"a\r\n  b\"\r\n",
	"run: \"a\r  b\"\r",
	"run: \"a\u0085  b\"\n",
	"run: \"a\u2028  b\"\n",
	"run: \"a\u2029  b\"\n",
	"run: 'trailing   \n  '\n",
	"---\nrun: plain\n...\n",
	">\n0",
	"|\n0\n",
	"run: !!str\n",
	"run: &empty !!str\ncopy: *empty\n",
	"run: !!str # empty\nnext: value\n",
	"first\nsecond\n",
	"\ufeffrun: hello\n",
	"!00 !00 0000",
	"&0000:0",
	"{0:}0",
	"{a:, b: c}\n",
	"[a:, b: c]\n",
	"!000000 *000000000",
	"!0\n ,",
}

func TestScalarDecoderInvariants(t *testing.T) {
	for _, input := range scalarDecoderInvariantCorpus {
		t.Run(input, func(t *testing.T) {
			root, err := codec.Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			checkScalarDecoderInvariants(t, input, root)
		})
	}
}

func FuzzScalarDecoderInvariants(f *testing.F) {
	for _, input := range scalarDecoderInvariantCorpus {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64*1024 {
			t.Skip()
		}
		root, err := codec.Parse([]byte(input))
		if err != nil {
			return
		}
		checkScalarDecoderInvariants(t, input, root)
	})
}

func checkScalarDecoderInvariants(t *testing.T, input string, root *syntax.Node) {
	t.Helper()
	p := workflowlang.NewSourceParser([]byte(input))
	p.IndexScalarSources(root)
	var visit func(*syntax.Node)
	visit = func(node *syntax.Node) {
		if node.Kind == syntax.ScalarNode {
			// An implicit empty value has no scalar token to locate.
			if node.Value == "" && node.Style == 0 {
				return
			}
			checkScalarDecoderMapping(t, p, node)
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(root)
}

func checkScalarDecoderMapping(t *testing.T, p *workflowlang.Parser, node *syntax.Node) {
	t.Helper()
	source := p.ScriptSource(node)
	if source == nil {
		t.Fatalf("missing scalar mapping at %d:%d: style=%d value=%q", node.Line, node.Column, node.Style, node.Value)
	}
	if source.Value != node.Value {
		t.Fatalf("decoded value %q differs from YAML parser value %q", source.Value, node.Value)
	}
	for _, pos := range []workflowlang.Pos{source.ContentStart, source.ScalarEnd} {
		if pos.Line < 1 || pos.Line > len(p.SourceLines) {
			t.Fatalf("scalar boundary outside source: %+v", pos)
		}
		if _, ok := text.ByteOffsetAtColumn(p.SourceLines[pos.Line-1], pos.Col); !ok {
			t.Fatalf("scalar boundary outside line: %+v", pos)
		}
	}
	if source.ScalarEnd.Line < source.ContentStart.Line || source.ScalarEnd.Line == source.ContentStart.Line && source.ScalarEnd.Col < source.ContentStart.Col {
		t.Fatalf("scalar ends before content starts: %+v -> %+v", source.ContentStart, source.ScalarEnd)
	}
	covered := make([]bool, len(source.Value))
	previousEnd := 0
	for _, span := range source.Spans {
		if span.Start < previousEnd || span.End < span.Start || span.End > len(source.Value) {
			t.Fatalf("unordered or out-of-bounds decoded span: %+v in %q", span, source.Value)
		}
		previousEnd = span.End
		if span.YamlLine < 1 || span.YamlLine > len(p.SourceLines) {
			t.Fatalf("span outside source lines: %+v", span)
		}
		rawLine := p.SourceLines[span.YamlLine-1]
		start, ok := text.ByteOffsetAtColumn(rawLine, span.YamlColumn)
		if !ok {
			t.Fatalf("span starts outside source line: %+v", span)
		}
		value := source.Value[span.Start:span.End]
		switch {
		case span.YamlEndLine != 0 && span.YamlEndLine != span.YamlLine:
			if span.YamlColumn != utf8.RuneCountInString(rawLine)+1 || span.YamlEndLine != span.YamlLine+1 || span.YamlEndColumn != 1 {
				t.Fatalf("invalid physical linebreak span: %+v", span)
			}
			if value != p.ScalarDocument.Breaks[span.YamlLine-1] {
				t.Fatalf("preserved linebreak %+v differs from source: %q", span, value)
			}
		case span.YamlEndColumn == 0:
			if !strings.HasPrefix(rawLine[start:], value) {
				t.Fatalf("copied span %+v decodes to %q, source is %q", span, value, rawLine[start:])
			}
		default:
			end, ok := text.ByteOffsetAtColumn(rawLine, span.YamlEndColumn)
			if !ok || end < start {
				t.Fatalf("escape ends outside source line: %+v", span)
			}
			quote := "\""
			if node.Style&syntax.SingleQuotedStyle != 0 {
				quote = "'"
			}
			encoded := []byte(quote + rawLine[start:end] + quote)
			root, err := codec.Parse(encoded)
			if err != nil {
				t.Fatalf("escape %+v cannot independently decode: %v", span, err)
			}
			session, cursor, err := codec.Open(root)
			if err != nil {
				t.Fatalf("escape %+v cannot open source graph: %v", span, err)
			}
			entered, err := cursor.Enter()
			if err != nil {
				session.Close()
				t.Fatalf("escape %+v cannot enter source scalar: %v", span, err)
			}
			var decoded string
			decodeErr := entered.Decode(&decoded)
			session.Close()
			if err := decodeErr; err != nil {
				t.Fatalf("escape %+v cannot independently decode: %v", span, err)
			}
			if decoded != value {
				t.Fatalf("escape %+v yields %q, mapped bytes are %q", span, decoded, value)
			}
		}
		for i := span.Start; i < span.End; i++ {
			covered[i] = true
		}
	}
	for i := 0; i < len(source.Value); {
		r, width := utf8.DecodeRuneInString(source.Value[i:])
		for j := i; j < i+width; j++ {
			if !covered[j] && r != ' ' && r != '\n' {
				t.Fatalf("decoded byte %d is unmapped and cannot come from YAML folding: %q", j, source.Value)
			}
		}
		i += width
	}
}
