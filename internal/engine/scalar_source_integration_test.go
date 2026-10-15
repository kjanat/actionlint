package engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"go.yaml.in/yaml/v4"
)

func TestPhysicalYAMLLineRendering(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", "\u0085", "\u2028", "\u2029"} {
		t.Run(fmt.Sprintf("%q", ending), func(t *testing.T) {
			src := strings.ReplaceAll("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: \"first\n        ${{ unknown }}\"\n", "\n", ending)
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range result.Diagnostics {
				if !strings.Contains(finding.Message, "undefined variable") {
					continue
				}
				t.Logf("diagnostic: %+v", finding)
				if !strings.Contains(finding.Snippet, "${{ unknown }}") {
					t.Errorf("physical YAML source line missing from snippet: %q", finding.Snippet)
				}
				var rendered bytes.Buffer
				failure := &Error{Line: finding.Start.Line, Column: finding.Start.Column, Message: finding.Message, Kind: finding.Rule, Filepath: "ci.yml"}
				failure.PrettyPrint(&rendered, []byte(src))
				if !strings.Contains(rendered.String(), "${{ unknown }}") {
					t.Errorf("physical YAML source line missing from text output: %q", rendered.String())
				}
				return
			}
			t.Fatal("missing finding")
		})
	}
}

func TestSourcedShellcheckPhysicalLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.without-extension")
	const first = "echo first\u2028still-first\u0085same-line\u2029same-line"
	const second = "echo $VALUE"
	content := []byte(first + "\n" + second + "\n")
	rule := &shellcheckRun{}
	for line, want := range []string{first, second} {
		finding := rule.sourcedDiagnostic(shellcheckError{File: path, Line: line + 1, Column: 1, Code: 2086, Message: "Quote expansion"}, "", map[string][]byte{path: content})
		if !finding.rawScriptSource {
			t.Fatal("sourced checker did not select raw script coordinates")
		}
		if got, ok := finding.getLine(content); !ok || got != want {
			t.Fatalf("line %d = %q, %v; want %q", line+1, got, ok, want)
		}
		fields := finding.GetTemplateFields([]byte("workflow source"))
		if !strings.HasPrefix(fields.Snippet, want+"\n") {
			t.Fatalf("sourced snippet = %q; want line %q", fields.Snippet, want)
		}
	}
}

func TestDiagnosticLongSourceLine(t *testing.T) {
	line := strings.Repeat("x", 70*1024)
	for _, raw := range []bool{false, true} {
		finding := &Error{Line: 2, Column: 1, rawScriptSource: raw}
		if got, ok := finding.getLine([]byte("first\n" + line + "\n")); !ok || got != line {
			t.Fatalf("raw=%v: long source line missing or truncated", raw)
		}
	}
}

func BenchmarkScalarWideFlow(b *testing.B) {
	for _, count := range []int{2000, 8000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			var source strings.Builder
			source.WriteString("env: {")
			for i := range count {
				fmt.Fprintf(&source, "K%d: '${{ github.actor }}',", i)
			}
			source.WriteString("}\n")
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(source.String()), &root); err != nil {
				b.Fatal(err)
			}
			mapping := root.Content[0].Content[1]
			content := []byte(source.String())
			b.SetBytes(int64(len(content)))
			b.ResetTimer()
			for b.Loop() {
				p := workflowlang.NewSourceParser(content)
				p.IndexScalarSources(&root)
				for i := 1; i < len(mapping.Content); i += 2 {
					if p.ScriptSource(mapping.Content[i]) == nil {
						b.Fatal("missing source")
					}
				}
			}
		})
	}
}
