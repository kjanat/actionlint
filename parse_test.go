package actionlint

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestParserScriptSource(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		node       *yaml.Node
		line       int
		column     int
		want       Pos
		wantMapped bool
	}{
		{
			name:   "literal block",
			source: "run: |2-\n  echo $foo\n    nested\nnext: value\n",
			node: &yaml.Node{
				Style:  yaml.LiteralStyle,
				Value:  "echo $foo\n  nested",
				Line:   1,
				Column: 6,
			},
			line:       1,
			column:     6,
			want:       Pos{Line: 2, Col: 8},
			wantMapped: true,
		},
		{
			name:   "indented literal line",
			source: "run: |2-\n  echo $foo\n    nested\nnext: value\n",
			node: &yaml.Node{
				Style:  yaml.LiteralStyle,
				Value:  "echo $foo\n  nested",
				Line:   1,
				Column: 6,
			},
			line:       2,
			column:     3,
			want:       Pos{Line: 3, Col: 5},
			wantMapped: true,
		},
		{
			name:   "leading blank literal line",
			source: "run: |\n\n  echo hi\nnext: value\n",
			node: &yaml.Node{
				Style:  yaml.LiteralStyle,
				Value:  "\necho hi\n",
				Line:   1,
				Column: 6,
			},
			line:       2,
			column:     1,
			want:       Pos{Line: 3, Col: 3},
			wantMapped: true,
		},
		{
			name:       "single-line plain scalar",
			source:     "run: echo $foo # YAML comment\n",
			node:       &yaml.Node{Value: "echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     6,
			want:       Pos{Line: 1, Col: 11},
			wantMapped: true,
		},
		{
			name:       "multiline plain scalar",
			source:     "run: echo first\n  echo $foo\nnext: value\n",
			node:       &yaml.Node{Value: "echo first echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     17,
			want:       Pos{Line: 2, Col: 8},
			wantMapped: true,
		},
		{
			name:       "synthetic whitespace in plain scalar",
			source:     "run: echo first\n  echo $foo\nnext: value\n",
			node:       &yaml.Node{Value: "echo first echo $foo", Line: 1, Column: 6},
			line:       1,
			column:     11,
			wantMapped: false,
		},
		{
			name:       "short continuation of plain scalar",
			source:     "step:\n  run: echo first\n    x\n",
			node:       &yaml.Node{Value: "echo first x", Line: 2, Column: 8},
			line:       1,
			column:     12,
			want:       Pos{Line: 3, Col: 5},
			wantMapped: true,
		},
		{
			name:       "plain scalar with unicode",
			source:     "run: echo é $foo\n",
			node:       &yaml.Node{Value: "echo é $foo", Line: 1, Column: 6},
			line:       1,
			column:     8,
			want:       Pos{Line: 1, Col: 13},
			wantMapped: true,
		},
		{
			name:       "folded block falls back",
			source:     "run: >\n  echo $foo\n",
			node:       &yaml.Node{Style: yaml.FoldedStyle, Value: "echo $foo\n", Line: 1, Column: 6},
			line:       1,
			column:     6,
			wantMapped: false,
		},
		{
			name:       "mismatched literal source falls back",
			source:     "run: |\n  echo source\n",
			node:       &yaml.Node{Style: yaml.LiteralStyle, Value: "echo decoded\n", Line: 1, Column: 6},
			line:       1,
			column:     1,
			wantMapped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &parser{sourceLines: splitSourceLines([]byte(tc.source))}
			source := p.scriptSource(tc.node)
			have, ok := source.pos(tc.line, tc.column)
			if ok != tc.wantMapped {
				t.Fatalf("mapped=%v but wanted %v (position=%v)", ok, tc.wantMapped, have)
			}
			if ok && *have != tc.want {
				t.Fatalf("mapped position is %v but wanted %v", have, tc.want)
			}
		})
	}

	t.Run("parser-backed literal block", func(t *testing.T) {
		input := "run: |\n  echo $foo\n"
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(input), &root); err != nil {
			t.Fatal(err)
		}
		run := root.Content[0].Content[1]
		p := &parser{sourceLines: splitSourceLines([]byte(input))}
		source := p.scriptSource(run)
		pos, ok := source.pos(1, 6)
		if !ok {
			t.Fatal("script position was not mapped")
		}
		if want := (Pos{Line: 2, Col: 8}); *pos != want {
			t.Fatalf("mapped position is %v but wanted %v", pos, want)
		}
		end, ok := source.endPos(1, 10)
		if !ok {
			t.Fatal("script end position was not mapped")
		}
		if want := (Pos{Line: 2, Col: 12}); *end != want {
			t.Fatalf("mapped end position is %v but wanted %v", end, want)
		}
	})
}

func BenchmarkParseWorkflow(b *testing.B) {
	type bench struct {
		name  string
		input []byte
	}

	loadBench := func(name string) bench {
		i, err := os.ReadFile(filepath.Join("testdata", "bench", name+".yaml"))
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
			for i := 0; i < b.N; i++ {
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
		_, fs, err := testFindAllWorkflowsInDir(name)
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
			for i := 0; i < b.N; i++ {
				for _, in := range bc.inputs {
					// Note: Some workflows may cause parse error
					Parse(in)
				}
			}
		})
	}
}
