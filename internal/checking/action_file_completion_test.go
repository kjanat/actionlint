package checking

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"
)

func TestUnknownActionFilesDoNotProveUnusedSuppression(t *testing.T) {
	t.Parallel()
	for _, standalone := range []bool{false, true} {
		for _, tc := range []struct{ unknown, disabled, extraSelector bool }{
			{false, false, false}, {true, false, false}, {false, false, true}, {true, false, true}, {true, true, false},
		} {
			unknown := tc.unknown
			t.Run(fmt.Sprintf("standalone=%t/unknown=%t/disabled=%t/extra=%t", standalone, unknown, tc.disabled, tc.extraSelector), func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				dir := filepath.Join(root, "local")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				file := "index.js"
				if unknown {
					file = "bad\x00.js"
				}
				selector := "action"
				if tc.extraSelector {
					selector += " env-var"
				}
				metadata := fmt.Sprintf("name: test\ndescription: test\nruns:\n  using: node24\n  main: %q # ignore[%s]\n", file, selector)
				path := filepath.Join(dir, "action.yml")
				if err := os.WriteFile(path, []byte(metadata), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "index.js"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", "local"}} {
					if output, err := gitclient.Command(t.Context(), "git", root, args...).CombinedOutput(); err != nil {
						t.Fatalf("prepare repository index: %v: %s", err, output)
					}
				}
				input := workspace.Source{Path: path, Content: []byte(metadata), Project: workspace.Identity(root)}
				if tc.disabled {
					cfg, err := ConfigurationRules().Parse([]byte("lint: {rules: {correctness: {action: off}}}\n"))
					if err != nil {
						t.Fatal(err)
					}
					input.Config = cfg
				}
				if !standalone {
					input.Path = filepath.Join(root, "ci.yml")
					input.Content = []byte("on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/local}, {uses: $/local}]}}\n")
				}
				result, err := Analyze(t.Context(), Options{WorkingDir: root, SkipProjectConfig: true}, []workspace.Source{input})
				if err != nil {
					t.Fatal(err)
				}
				unused := 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule != "unused-suppression" {
						t.Fatalf("unexpected finding: %+v", diagnostic)
					}
					unused++
					if unknown && tc.extraSelector {
						if diagnostic.Message != `inline suppression of "env-var" did not match any findings` || len(diagnostic.Fixes) == 0 {
							t.Fatalf("cleanup targeted an uncertain selector: %+v", diagnostic)
						}
						for _, fix := range diagnostic.Fixes {
							for _, edit := range fix.Edits {
								if !strings.Contains(edit.Replacement, "action") || strings.Contains(edit.Replacement, "env-var") {
									t.Fatalf("cleanup removed the uncertain selector: %+v", edit)
								}
							}
						}
					}
				}
				wantUnused := 0
				if !unknown || tc.disabled {
					wantUnused++
				}
				if tc.extraSelector {
					wantUnused++
				}
				if unused != wantUnused {
					t.Fatalf("wrong suppression cleanup: unknown=%t unused=%d", unknown, unused)
				}
				wantIncomplete := 0
				if unknown && !tc.disabled {
					wantIncomplete = 2
					if standalone {
						wantIncomplete = 1
					}
				}
				if len(result.IncompleteChecks) != wantIncomplete {
					t.Fatalf("lost partial action checks: %+v", result.IncompleteChecks)
				}
				for _, check := range result.IncompleteChecks {
					if check.Rule != "action" || check.Source.Document == "" || check.Source.Revision == "" || check.Invocation.ID == "" || check.Reason == "" {
						t.Fatalf("partial check lost its identity: %+v", check)
					}
					if check.Invocation.Entry.Document == "" || check.Invocation.Entry.Revision == "" || !strings.HasSuffix(filepath.ToSlash(check.Source.Document), "/local/action.yml") {
						t.Fatalf("partial check identified the wrong source: %+v", check)
					}
				}
				if wantIncomplete == 2 && result.IncompleteChecks[0].Invocation.ID == result.IncompleteChecks[1].Invocation.ID {
					t.Fatal("repeated calls share one completion record")
				}
			})
		}
	}
}
