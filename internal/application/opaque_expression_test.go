package application

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestAnalyzeOpaqueCompositeInputsPublishCapturedExpressionFindings(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		for _, cycle := range []bool{false, true} {
			for _, sink := range []struct{ expression, rule string }{{"github.event.issue.title", "script-injection"}, {"github.repository", "script-interpolation"}} {
				t.Run(fmt.Sprintf("standalone=%t/cycle=%t/%s", standalone, cycle, sink.rule), func(t *testing.T) {
					root := t.TempDir()
					path := filepath.Join(root, "outer", "action.yml")
					content := "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n"
					if cycle {
						content += "    - uses: $/outer\n"
					}
					content += "    - uses: actions/github-script@v7\n      with:\n        script: ${{ " + sink.expression + " }}\n"
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
					unit := sourceUnit{Path: "ci.yml", Content: []byte("on: issues\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n"), Project: workspace.Identity(root)}
					if standalone {
						unit.Path, unit.Content = filepath.Join("outer", "action.yml"), []byte(content)
					}
					result, err := Analyze(t.Context(), Options{WorkingDir: root}, []sourceUnit{unit})
					if err != nil || result == nil {
						t.Fatalf("opaque composite analysis failed: %v", err)
					}
					captured, present := result.CapturedSources[path]
					if !present {
						t.Fatal("declaring manifest has no sealed capture")
					}
					matched := 0
					for index, finding := range result.Diagnostics {
						if finding.Rule != sink.rule {
							continue
						}
						matched++
						if finding.Reference == nil || finding.Reference.Document != captured.Revision.Document() || finding.Reference.Revision != captured.Revision.Digest() || !bytes.Equal(result.Sources[index].Content, []byte(content)) || finding.Start.Line < 1 || finding.Start.Column < 1 || result.Sources[index].Unavailable || result.Sources[index].RawScript {
							t.Fatalf("published sink lost captured YAML evidence: %+v", finding)
						}
					}
					if matched != 1 {
						t.Fatalf("opaque composite sink omitted or duplicated: %+v", result.Diagnostics)
					}
				})
			}
		}
	}
}
