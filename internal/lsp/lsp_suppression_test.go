package lsp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	reportfixture "actionlint.kjanat.dev/v2/internal/development/tests/support/reportfixture"

	"actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestEditorUnusedSuppressionQuickFix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	uri := FileURI(path)
	const line = "      - run: echo 😀 # ignore[expression-reference, script-interpolation]"
	const text = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n" + line + "\n" + "\npermissions: {}\n"
	publication, logs := firstEditorPublication(t, func(ctx context.Context, documents map[string]Document) (Analysis, error) {
		return analyzeDocuments(ctx, checking.Options{WorkingDir: root, SkipProjectConfig: true}, documents)
	}, uri, text, func(send func(string, int, any), read func() editorPublication) {
		send("textDocument/codeAction", 3, map[string]any{
			"textDocument": map[string]string{"uri": uri},
			"range": map[string]any{
				"start": map[string]int{"line": 5, "character": 0},
				"end":   map[string]int{"line": 5, "character": len(utf16.Encode([]rune(line)))},
			},
		})
		response := read()
		var actions []struct {
			Title       string
			IsPreferred bool
			Edit        struct {
				DocumentChanges []struct {
					TextDocument struct {
						URI     string
						Version int
					}
					Edits []struct {
						Range   struct{ Start, End struct{ Line, Character int } }
						NewText string
					}
				}
			}
		}
		if err := json.Unmarshal(response.Result, &actions); err != nil || response.ID != 3 || len(actions) != 1 {
			t.Fatalf("unused selectors did not share one editor fix: %s, %v", response.Result, err)
		}
		action := actions[0]
		if action.Title != "Remove unused suppression" || !action.IsPreferred || len(action.Edit.DocumentChanges) != 1 {
			t.Fatalf("cleanup is not a preferred fix: %+v", action)
		}
		change := action.Edit.DocumentChanges[0]
		if change.TextDocument.URI != uri || change.TextDocument.Version != 17 || len(change.Edits) != 1 {
			t.Fatalf("cleanup lost buffer identity: %+v", change)
		}
		edit := change.Edits[0]
		if edit.NewText != "" || edit.Range.Start.Line != 5 || edit.Range.Start.Character != 20 || edit.Range.End.Line != 5 || edit.Range.End.Character != len(utf16.Encode([]rune(line))) {
			t.Fatalf("cleanup did not preserve Unicode YAML content: %+v", edit)
		}
	})
	if len(logs) != 0 || len(publication.Params.Diagnostics) != 2 {
		t.Fatalf("missing unused suppression diagnostics: %+v, %v", publication, logs)
	}
}

func TestEditorSuppressionQuickFix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	uri := FileURI(path)
	const text = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo '::set-output name=result::value'\n        shell: bash\npermissions: {}\n"
	options := checking.Options{WorkingDir: root, SkipProjectConfig: true}
	publication, logs := firstEditorPublication(t, func(ctx context.Context, documents map[string]Document) (Analysis, error) {
		return analyzeDocuments(ctx, options, documents)
	}, uri, text, func(send func(string, int, any), read func() editorPublication) {
		send("textDocument/codeAction", 3, map[string]any{
			"textDocument": map[string]string{"uri": uri},
			"range":        map[string]any{"start": map[string]int{"line": 6, "character": 0}, "end": map[string]int{"line": 6, "character": 55}},
		})
		response := read()
		var actions []struct {
			Title       string `json:"title"`
			Kind        string `json:"kind"`
			IsPreferred bool   `json:"isPreferred"`
			Edit        struct {
				DocumentChanges []struct {
					TextDocument struct {
						URI     string `json:"uri"`
						Version int    `json:"version"`
					} `json:"textDocument"`
					Edits []struct {
						Range struct {
							Start, End struct{ Line, Character int }
						} `json:"range"`
						NewText string `json:"newText"`
					} `json:"edits"`
				} `json:"documentChanges"`
			} `json:"edit"`
		}
		if response.ID != 3 {
			t.Fatalf("unexpected action response: %+v", response)
		}
		if err := json.Unmarshal(response.Result, &actions); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, action := range actions {
			if action.Title != "Suppress deprecated-commands for this declaration" {
				continue
			}
			found = true
			if action.Kind != "quickfix" || action.IsPreferred || len(action.Edit.DocumentChanges) != 1 {
				t.Fatalf("invalid suppression action: %+v", action)
			}
			change := action.Edit.DocumentChanges[0]
			if change.TextDocument.URI != uri || change.TextDocument.Version != 17 || len(change.Edits) != 1 {
				t.Fatalf("suppression lost its buffer identity: %+v", change)
			}
			edit := change.Edits[0]
			if edit.Range.Start.Line != 5 || edit.Range.Start.Character != 0 || edit.Range.End != edit.Range.Start || edit.NewText != "      # actionlint-ignore-next-line deprecated-commands\n" {
				t.Fatalf("suppression was not attached to the block header: %+v", edit)
			}
			lines := strings.SplitAfter(text, "\n")
			updated := strings.Join(lines[:5], "") + edit.NewText + strings.Join(lines[5:], "")
			send("textDocument/didChange", 0, map[string]any{
				"textDocument":   map[string]any{"uri": uri, "version": 18},
				"contentChanges": []map[string]string{{"text": updated}},
			})
			publication := read()
			if publication.Method != "textDocument/publishDiagnostics" || publication.Params.Version != 18 || len(publication.Params.Diagnostics) != 0 {
				t.Fatalf("applying suppression did not clear the diagnostic: %+v", publication)
			}
		}
		if !found {
			t.Fatalf("no suppression quick fix: %s", response.Result)
		}
	})
	if len(logs) != 0 || len(publication.Params.Diagnostics) != 1 {
		t.Fatalf("fixture did not publish its diagnostic: %+v, %v", publication, logs)
	}
}

func TestEditorSuppressionRespectsPolicy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte("policy: {disallow-suppressions: true}\n"))
	if err != nil {
		t.Fatal(err)
	}
	text := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: \"echo '::set-output name=x::y'\"}]}}\npermissions: {}\n"
	analysis, err := analyzeDocuments(t.Context(), checking.Options{WorkingDir: root, SkipProjectConfig: true, ConfigOverlays: []repoconfig.Overlay{overlay}}, map[string]Document{path: {URI: FileURI(path), Version: 1, Text: text}})
	if err != nil || len(analysis.Diagnostics) != 1 {
		t.Fatalf("fixture analysis: %+v, %v", analysis.Diagnostics, err)
	}
	for _, fix := range analysis.Diagnostics[0].Fixes {
		if strings.HasPrefix(fix.Description, "Suppress ") {
			t.Fatalf("offered a prohibited suppression: %+v", fix)
		}
	}
}

func TestEditorSuppressionTargetsCapturedOrigin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workflow, script := filepath.Join(root, "workflow.yml"), filepath.Join(root, "check.sh")
	yaml := []byte("run: |\n  . ./check.sh\n")
	raw := []byte("echo $VALUE\n")
	for _, available := range []bool{true, false} {
		captured := reportfixture.CapturedSource(t, workflow, yaml, sourcepos.YAML)
		result := &checking.Result{
			Diagnostics:     []results.Diagnostic{{Path: script, Rule: "plugin-check", Start: sourcepos.Position{Line: 1, Column: 1}}},
			Sources:         []report.DiagnosticSource{{Content: raw, RawScript: true, Suppression: &report.SuppressionTarget{Path: workflow, First: 1, Last: 2}}},
			CapturedSources: map[string]report.CapturedSource{script: reportfixture.CapturedSource(t, script, raw, sourcepos.YAML)},
			Rules:           []rule.Descriptor{{Name: "plugin-check"}},
		}
		if available {
			result.CapturedSources[workflow] = captured
		}
		analysis, err := FromReport(result, root, map[string]Document{workflow: {URI: FileURI(workflow), Version: 8, Text: string(yaml)}})
		if err != nil || len(analysis.Diagnostics) != 1 {
			t.Fatalf("origin fixture failed: %+v, %v", analysis.Diagnostics, err)
		}
		fixes := analysis.Diagnostics[0].Fixes
		if !available {
			if len(fixes) != 0 {
				t.Fatalf("uncaptured origin authorized an edit: %+v", fixes)
			}
			continue
		}
		if len(fixes) != 1 || len(fixes[0].Edits) != 1 {
			t.Fatalf("plugin diagnostic has no origin suppression: %+v", fixes)
		}
		fix := fixes[0]
		if fix.Edits[0].Path != workflow || fix.Revisions[workflow] != captured.Revision.Digest() || fix.Edits[0].Replacement != "# actionlint-ignore-next-line plugin-check\n" {
			t.Fatalf("suppression edited the script instead of captured YAML: %+v", fix)
		}
		if len(result.Diagnostics[0].Fixes) != 0 {
			t.Fatal("editor-only suppression leaked into CLI fixes")
		}
	}
}
