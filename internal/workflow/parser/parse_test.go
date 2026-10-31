package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestParseYAMLIntegerRadixBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"0x0", 0},
		{"0x7fffffff", 2147483647},
		{"0x80000000", -2147483648},
		{"0xffffffff", -1},
		{"0o0", 0},
		{"0o17777777777", 2147483647},
		{"0o20000000000", -2147483648},
		{"0o37777777777", -1},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseYAMLInteger(tc.value)
			if err != nil || got != tc.want {
				t.Fatalf("got (%d, %v), want (%d, nil)", got, err, tc.want)
			}
		})
	}
	for _, value := range []string{"0x100000000", "0o40000000000", "0x", "0o8"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseYAMLInteger(value); err == nil {
				t.Fatal("expected invalid or overflowing integer to fail")
			}
		})
	}
}

func TestParserRejectsInvalidMappingKeys(t *testing.T) {
	doc, err := codec.Parse([]byte(`!!int first: {nested: !!bool nope}
!!bool second: value
'': empty
valid: kept
VALID: duplicate
`))
	if err != nil {
		t.Fatal(err)
	}
	p := &Parser{}
	var keys []string
	for e := range p.parseSectionMapping("test", doc.Content[0], false, false) {
		keys = append(keys, e.id)
	}
	if len(keys) != 1 || keys[0] != "valid" {
		t.Fatalf("invalid keys entered mapping: %v", keys)
	}
	want := []string{
		`invalid value "first" for "!!int" tag`,
		`invalid value "nope" for "!!bool" tag`,
		`invalid value "second" for "!!bool" tag`,
		`string should not be empty`,
		`key "VALID" is duplicated`,
	}
	if len(p.errs) != len(want) {
		t.Fatalf("want %d diagnostics, got %v", len(want), p.errs)
	}
	for i, message := range want {
		if !strings.HasPrefix(p.errs[i].Message, message) {
			t.Errorf("diagnostic %d: want %q, got %s", i, message, p.errs[i])
		}
	}
	for _, source := range []string{"!!int invalid: value", "? [invalid]\n: value"} {
		t.Run(source, func(t *testing.T) {
			doc, err := codec.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p := &Parser{}
			for entry := range p.parseSectionMapping("test", doc.Content[0], false, false) {
				t.Fatalf("invalid entry was returned: %v", entry)
			}
			if len(p.errs) != 1 || strings.Contains(p.errs[0].Message, "should not be empty") {
				t.Fatalf("want only the invalid key diagnostic: %v", p.errs)
			}
		})
	}
}

func TestParserAnchorNames(t *testing.T) {
	for _, name := range []string{"git+opts", "git-opts", "git_opts", "opts123"} {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: &%s echo test
      - run: *%s
`, name, name)
			_, errs := Parse([]byte(src))
			if name != "git+opts" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				return
			}
			if len(errs) != 2 {
				t.Fatalf("wanted anchor and alias errors, got %v", errs)
			}
			for i, err := range errs {
				if err.Line != 6+i || err.Column != 14 || !strings.Contains(err.Message, `"git+opts"`) {
					t.Fatalf("unexpected anchor diagnostic: %v", err)
				}
			}
		})
	}
}

func TestParserAliasTypeLocations(t *testing.T) {
	tests := []struct {
		name, value, message string
		check                func(*Parser, *syntax.Node)
	}{
		{"string", "{}", "expected scalar node for string value", func(p *Parser, n *syntax.Node) { p.parseString(n, false) }},
		{"cache mode", "{}", "expected string for \"cache-mode\"", func(p *Parser, n *syntax.Node) { p.parseCacheMode(n) }},
		{"sequence", "{}", "must be sequence node", func(p *Parser, n *syntax.Node) { p.checkSequence("steps", n, false) }},
		{"mapping", "[]", "mapping node is expected", func(p *Parser, n *syntax.Node) {
			for range p.parseSectionMapping("env", n, false, false) {
			}
		}},
		{"bool", "{}", "expected bool value", func(p *Parser, n *syntax.Node) { p.parseBool(n) }},
		{"integer", "{}", "expected scalar node for integer value", func(p *Parser, n *syntax.Node) { p.parseInt(n) }},
		{"float", "{}", "expected scalar node for float value", func(p *Parser, n *syntax.Node) { p.parseFloat(n) }},
		{"expression", "plain", "expecting a single ${{...}} expression", func(p *Parser, n *syntax.Node) { p.parseBool(n) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "anchor: &value " + tt.value + `
first: *value
second: *value
`
			doc, err := codec.Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			p := &Parser{}
			projected := projectAliasTestInput(p, doc)
			for i, n := range []*syntax.Node{projected.Content[0].Content[3], projected.Content[0].Content[5]} {
				if p.aliases[n] != doc.Content[0].Content[3+2*i] || doc.Content[0].Content[3+2*i].Kind != syntax.AliasNode {
					t.Fatal("type diagnostic lost original alias provenance")
				}
				tt.check(p, n)
			}
			if len(p.errs) != 2 {
				t.Fatalf("wanted one error per alias use, got %v", p.errs)
			}
			for i, err := range p.errs {
				if err.Line != i+2 || err.Column != i+8 {
					t.Errorf("error points to %d:%d, want %d:%d", err.Line, err.Column, i+2, i+8)
				}
				if !strings.Contains(err.Message, tt.message) || !strings.Contains(err.Message, `alias "value" refers to the anchor at line:1, column:9`) {
					t.Errorf("error lost the type mismatch or anchor location: %s", err.Message)
				}
			}
		})
	}
}

func TestParserScriptSource(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		node       *syntax.Node
		line       int
		column     int
		want       workflowlang.Pos
		wantMapped bool
	}{
		{
			name: "literal block",
			source: `run: |2-
  echo $foo
    nested
next: value
`,
			node: &syntax.Node{
				Style:  syntax.LiteralStyle,
				Value:  "echo $foo\n  nested",
				Line:   1,
				Column: 6,
			},
			line:       1,
			column:     6,
			want:       workflowlang.Pos{Line: 2, Col: 8},
			wantMapped: true,
		},
		{
			name: "indented literal line",
			source: `run: |2-
  echo $foo
    nested
next: value
`,
			node: &syntax.Node{
				Style:  syntax.LiteralStyle,
				Value:  "echo $foo\n  nested",
				Line:   1,
				Column: 6,
			},
			line:       2,
			column:     3,
			want:       workflowlang.Pos{Line: 3, Col: 5},
			wantMapped: true,
		},
		{
			name: "leading blank literal line",
			source: `run: |

  echo hi
next: value
`,
			node: &syntax.Node{
				Style:  syntax.LiteralStyle,
				Value:  "\necho hi\n",
				Line:   1,
				Column: 6,
			},
			line:       2,
			column:     1,
			want:       workflowlang.Pos{Line: 3, Col: 3},
			wantMapped: true,
		},
		{
			name:       "single-line plain scalar",
			source:     "run: echo $foo # YAML comment\n",
			node:       &syntax.Node{Value: "echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     6,
			want:       workflowlang.Pos{Line: 1, Col: 11},
			wantMapped: true,
		},
		{
			name: "multiline plain scalar",
			source: `run: echo first
  echo $foo
next: value
`,
			node:       &syntax.Node{Value: "echo first echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     17,
			want:       workflowlang.Pos{Line: 2, Col: 8},
			wantMapped: true,
		},
		{
			name: "synthetic whitespace in plain scalar",
			source: `run: echo first
  echo $foo
next: value
`,
			node:       &syntax.Node{Value: "echo first echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     11,
			wantMapped: false,
		},
		{
			name: "short continuation of plain scalar",
			source: `step:
  run: echo first
    x
`,
			node:       &syntax.Node{Value: "echo first x", Line: 2, Column: 8},
			line:       1,
			column:     12,
			want:       workflowlang.Pos{Line: 3, Col: 5},
			wantMapped: true,
		},
		{
			name:       "plain scalar with unicode",
			source:     "run: echo é $foo\n",
			node:       &syntax.Node{Value: "echo é $foo", Line: 1, Column: 6},
			line:       1,
			column:     8,
			want:       workflowlang.Pos{Line: 1, Col: 13},
			wantMapped: true,
		},
		{
			name: "folded block",
			source: `run: >
  echo $foo
`,
			node:       &syntax.Node{Style: syntax.FoldedStyle, Value: "echo $foo\n", Line: 1, Column: 6},
			line:       1,
			column:     6,
			want:       workflowlang.Pos{Line: 2, Col: 8},
			wantMapped: true,
		},
		{
			name: "mismatched literal source falls back",
			source: `run: |
  echo source
`,
			node:       &syntax.Node{Style: syntax.LiteralStyle, Value: "echo decoded\n", Line: 1, Column: 6},
			line:       1,
			column:     1,
			wantMapped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewSourceParser([]byte(tc.source))
			source := p.ScriptSource(tc.node)
			var have *workflowlang.Pos
			var ok bool
			if source != nil {
				have, ok = source.Position(tc.line, tc.column)
			}
			if ok != tc.wantMapped {
				t.Fatalf("mapped=%v but wanted %v (position=%v)", ok, tc.wantMapped, have)
			}
			if ok && *have != tc.want {
				t.Fatalf("mapped position is %v but wanted %v", have, tc.want)
			}
		})
	}

	t.Run("parser-backed literal block", func(t *testing.T) {
		input := `run: |
  echo $foo
`
		root, err := codec.Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		run := root.Content[0].Content[1]
		p := NewSourceParser([]byte(input))
		source := p.ScriptSource(run)
		pos, ok := source.Position(1, 6)
		if !ok {
			t.Fatal("script position was not mapped")
		}
		if want := (workflowlang.Pos{Line: 2, Col: 8}); *pos != want {
			t.Fatalf("mapped position is %v but wanted %v", pos, want)
		}
		end, ok := source.EndPos(1, 10)
		if !ok {
			t.Fatal("script end position was not mapped")
		}
		if want := (workflowlang.Pos{Line: 2, Col: 12}); *end != want {
			t.Fatalf("mapped end position is %v but wanted %v", end, want)
		}
	})

	t.Run("aliased literal block", func(t *testing.T) {
		input := `script: &script |
  echo $foo
run: *script
`
		root, err := codec.Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		p := NewSourceParser([]byte(input))
		p.IndexScalarSources(root)
		projected := projectAliasTestInput(p, root)
		if len(p.errs) != 0 {
			t.Fatal(p.errs)
		}
		originalAlias := root.Content[0].Content[3]
		if p.aliases[projected.Content[0].Content[3]] != originalAlias || originalAlias.Kind != syntax.AliasNode {
			t.Fatal("script source lost original alias provenance")
		}
		for _, n := range []*syntax.Node{projected.Content[0].Content[1], projected.Content[0].Content[3]} {
			source := p.ScriptSource(n)
			if source == nil {
				t.Fatal("literal script lost its source mapping")
			}
			pos, ok := source.Position(1, 6)
			if want := (workflowlang.Pos{Line: 2, Col: 8}); !ok || *pos != want {
				t.Fatalf("script position is %v (mapped=%v), want %v", pos, ok, want)
			}
		}
	})

	t.Run("parser-backed plain scalar with blank continuation", func(t *testing.T) {
		input := `run: echo first

  echo $foo
`
		root, err := codec.Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		run := root.Content[0].Content[1]
		p := NewSourceParser([]byte(input))
		pos, ok := p.ScriptSource(run).Position(2, 6)
		if !ok {
			t.Fatal("script position after blank continuation was not mapped")
		}
		if want := (workflowlang.Pos{Line: 3, Col: 8}); *pos != want {
			t.Fatalf("mapped position is %v but wanted %v", pos, want)
		}
	})
}

func BenchmarkParseWorkflow(b *testing.B) {
	type bench struct {
		name  string
		input []byte
	}

	loadBench := func(name string) bench {
		i, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "bench", name+".yaml"))
		if err != nil {
			b.Fatal(err)
		}
		return bench{name, i}
	}

	for _, bc := range []bench{
		loadBench("minimal"),
		loadBench("small"),
		loadBench("large"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for range b.N {
				if _, errs := Parse(bc.input); len(errs) > 0 {
					b.Fatal(errs)
				}
			}
		})
	}
}

func BenchmarkParseTestData(b *testing.B) {
	type bench struct {
		name   string
		inputs [][]byte
	}

	loadBench := func(name string) bench {
		inputs := [][]byte{}
		fs, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", name, "*.y*ml"))
		if err != nil {
			b.Fatal(err)
		}
		for _, f := range fs {
			bs, err := os.ReadFile(f)
			if err != nil {
				b.Fatal(err)
			}
			inputs = append(inputs, bs)
		}
		return bench{name, inputs}
	}

	for _, bc := range []bench{
		loadBench("examples"),
		loadBench("ok"),
		loadBench("err"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for range b.N {
				for _, in := range bc.inputs {
					// Note: Some workflows may cause parse error
					Parse(in)
				}
			}
		})
	}
}

func TestParseAliasedScriptsKeepPhysicalScalarContext(t *testing.T) {
	for _, tc := range []struct {
		name, scalar, value                   string
		tokenLine, decodedLine, decodedColumn int
	}{
		{"literal", "|-\n    echo first\n    $foo\n", "echo first\n$foo", 5, 2, 1},
		{"folded", ">-\n    echo first\n    $foo\n", "echo first $foo", 5, 1, 12},
		{"multiline plain", "echo first\n    $foo\n", "echo first $foo", 4, 1, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := "on: push\nenv:\n  SCRIPT: &script " + tc.scalar + "jobs:\n  block:\n    runs-on: ubuntu-latest\n    steps:\n      - run: *script\n  flow:\n    runs-on: ubuntu-latest\n    steps: [{run: *script}]\n"
			workflow, diagnostics := Parse([]byte(input))
			if len(diagnostics) != 0 || workflow == nil {
				t.Fatalf("Parse diagnostics=%v", diagnostics)
			}
			wantStart, wantEnd := workflowlang.Pos{Line: tc.tokenLine, Col: 5}, workflowlang.Pos{Line: tc.tokenLine, Col: 9}
			for _, name := range []string{"block", "flow"} {
				job := workflow.Jobs[name]
				if job == nil || len(job.Steps) != 1 {
					t.Fatalf("job %q missing run step", name)
				}
				switch run := job.Steps[0].Exec.(type) {
				case *workflowlang.ExecRun:
					if run.Run == nil || run.Run.Value != tc.value || run.Source == nil || run.Source.Value != tc.value {
						t.Fatalf("job %q run/source=%+v", name, run)
					}
					if got, ok := run.Source.Position(tc.decodedLine, tc.decodedColumn); !ok || *got != wantStart {
						t.Errorf("job %q token=%v mapped=%t want %v", name, got, ok, wantStart)
					}
					if got, ok := run.Source.EndPos(tc.decodedLine, tc.decodedColumn+4); !ok || *got != wantEnd {
						t.Errorf("job %q token end=%v mapped=%t want %v", name, got, ok, wantEnd)
					}
					if got, ok := run.Source.EndPos(tc.decodedLine, len(strings.Split(tc.value, "\n")[tc.decodedLine-1])+1); !ok || *got != wantEnd {
						t.Errorf("job %q script end=%v mapped=%t want %v", name, got, ok, wantEnd)
					}
				default:
					t.Fatalf("job %q exec=%T want run", name, run)
				}
			}
			// Prove the fixture detects indexing projected aliases: the final flow use overwrites the anchor context.
			root, err := codec.Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			wrong := NewSourceParser([]byte(input))
			projected := projectAliasTestInput(wrong, root)
			wrong.IndexScalarSources(projected)
			var projectedScript *syntax.Node
			var find func(*syntax.Node)
			find = func(node *syntax.Node) {
				if node.Kind == syntax.ScalarNode && node.Anchor == "script" {
					projectedScript = node
				}
				for _, child := range node.Content {
					find(child)
				}
			}
			find(projected)
			if projectedScript == nil || !wrong.scalarContexts[*PosAt(projectedScript)].Flow {
				t.Fatal("fixture did not overwrite anchor context with final flow alias")
			}
			wrongSource := wrong.ScriptSource(projectedScript)
			if wrongSource != nil {
				start, startOK := wrongSource.Position(tc.decodedLine, tc.decodedColumn)
				end, endOK := wrongSource.EndPos(tc.decodedLine, tc.decodedColumn+4)
				if startOK && endOK && *start == wantStart && *end == wantEnd {
					t.Fatal("fixture does not distinguish projected source indexing")
				}
			}
		})
	}
}
