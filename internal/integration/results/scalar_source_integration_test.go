package results_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/buildinfo"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestSourcedScriptRenderingPreservesPhysicalLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "library.sh")
	if err := os.WriteFile(path, []byte("echo first\u2028still-first\u0085same-line\u2029same-line\necho $VALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := application.Analyze(t.Context(), application.Options{
		WorkingDir: dir,
		Tools: (adapters.Options{
			ShellCheck: "shellcheck",

			ShellcheckRunner: func([]string, string) ([]byte, error) {
				return fmt.Appendf(nil, `{"comments":[{"file":%s,"line":2,"column":6,"endLine":2,"endColumn":12,"level":"info","code":2086,"message":"Quote expansion"}]}`, strconv.Quote(filepath.ToSlash(path))), nil
			},
		}).Registrations(),
	}, []workspace.Source{{Path: "ci.yml", Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Snippet != "echo $VALUE" {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	for _, template := range []string{"", "{{range .}}{{.Snippet}}{{end}}"} {
		renderer, err := render.New("", template, false, buildinfo.Version, nil)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := renderer.Render(&out, render.Prepare(*result)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "echo $VALUE") || strings.Contains(out.String(), "still-first") {
			t.Fatalf("wrong source line: %s", out.String())
		}
	}
}

func TestPhysicalYAMLLineRendering(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", "\u0085", "\u2028", "\u2029"} {
		t.Run(fmt.Sprintf("%q", ending), func(t *testing.T) {
			src := strings.ReplaceAll("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: \"first\n        ${{ unknown }}\"\n", "\n", ending)
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
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
				failure := &findingmodel.Finding{Start: diagmodel.Position{Line: finding.Start.Line, Column: finding.Start.Column}, Message: finding.Message, Rule: finding.Rule, Path: "ci.yml"}
				render.FindingOf(failure.Diagnostic, []byte(src)).PrettyPrint(&rendered)
				if !strings.Contains(rendered.String(), "${{ unknown }}") {
					t.Errorf("physical YAML source line missing from text output: %q", rendered.String())
				}
				return
			}
			t.Fatal("missing finding")
		})
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
			root, err := codec.Parse([]byte(source.String()))
			if err != nil {
				b.Fatal(err)
			}
			mapping := root.Content[0].Content[1]
			content := []byte(source.String())
			b.SetBytes(int64(len(content)))
			b.ResetTimer()
			for b.Loop() {
				p := workflowparser.NewSourceParser(content)
				p.IndexScalarSources(root)
				for i := 1; i < len(mapping.Content); i += 2 {
					if p.ScriptSource(mapping.Content[i]) == nil {
						b.Fatal("missing source")
					}
				}
			}
		})
	}
}
