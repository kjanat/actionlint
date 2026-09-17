package actionlint

import (
	"io"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestExpressionWhitespaceWorkflowBoundaries(t *testing.T) {
	// TemplateReader preserves whitespace outside expression delimiters as text.
	// https://github.com/actions/runner/blob/fee24199cba8acf6c25da3a061c986066f42327a/src/Sdk/DTObjectTemplating/ObjectTemplating/TemplateReader.cs#L563-L593
	for _, tc := range []struct{ name, field, message string }{
		{"quoted environment", `environment: "  ${{ '' }}  "`, ""},
		{"block environment", "environment: |\n      ${{ '' }}", ""},
		{"plain environment", "environment:   ${{ '' }}   ", "must be a non-empty string"},
		{"exact environment", "environment: ${{ 'production' }}", ""},
		{"quoted env mapping", `env: " ${{ fromJSON('{}') }} "`, "expecting a single"},
		{"quoted unknown mapping", `env: " ${{ fromJSON(vars.ENVIRONMENT) }} "`, "expecting a single"},
		{"block env mapping", "env: |\n      ${{ fromJSON('{}') }}", "expecting a single"},
		{"plain env mapping", "env:   ${{ fromJSON('{}') }}   ", ""},
		{"stripped block env mapping", "env: |-\n      ${{ fromJSON('{}') }}", ""},
		{"bare if whitespace", `if: "  github.ref == 'refs/heads/main'  "`, ""},
		{"interpolated if", `if: "  ${{ false }}  "`, "always evaluated to true because extra characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    " + tc.field + "\n    steps:\n      - run: echo ok\n"
			errs, err := linter.Lint("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.message == "" {
				if len(errs) != 0 {
					t.Fatalf("valid workflow rejected: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.message) {
				t.Fatalf("want one %q diagnostic, got %v", tc.message, errs)
			}
		})
	}
}

func TestExpressionWhitespaceStaticUses(t *testing.T) {
	for _, tc := range []struct {
		name, value, parsed string
		invalid             bool
	}{
		{"plain", "  ${{ './action' }}  ", "./action", false},
		{"quoted", `"  ${{ './action' }}  "`, "  ${{ './action' }}  ", true},
		{"block", "|\n          ${{ './action' }}", "${{ './action' }}\n", true},
		{"stripped block", "|-\n          ${{ './action' }}", "./action", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + tc.value + "\n")
			workflow, errs := Parse(source)
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			uses := workflow.Jobs["check"].Steps[0].Exec.(*ExecAction).Uses
			if uses.Value != tc.parsed {
				t.Fatalf("decoded uses changed: got %q, want %q", uses.Value, tc.parsed)
			}
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			errs, err = linter.Lint("workflow.yml", source, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if len(errs) != 1 || !strings.Contains(errs[0].Message, "expressions are not allowed in this static workflow field") {
					t.Fatalf("want static interpolation diagnostic, got %v", errs)
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid static reference rejected: %v", errs)
			}
		})
	}
}

func TestExpressionWhitespaceActionMappingBoundaries(t *testing.T) {
	for _, field := range []string{"env", "with"} {
		for _, tc := range []struct {
			name, value string
			invalid     bool
		}{
			{"quoted map", `" ${{ fromJSON('{}') }} "`, true},
			{"quoted unknown map", `" ${{ fromJSON(inputs.value) }} "`, true},
			{"block map", "|\n        ${{ fromJSON('{}') }}", true},
			{"plain map", "  ${{ fromJSON('{}') }}  ", false},
			{"stripped block map", "|-\n        ${{ fromJSON('{}') }}", false},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				source := []byte("name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n      " + field + ": " + tc.value + "\n")
				var meta ActionMetadata
				if err := yaml.Unmarshal(source, &meta); err != nil {
					t.Fatal(err)
				}
				meta.file, meta.src = "action.yml", source
				rule := NewRuleAction(nil)
				rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./action", Pos: &Pos{Line: 1, Col: 1}}})
				errs := rule.Errs()
				if tc.invalid {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "expected a mapping") {
						t.Fatalf("want one mapping diagnostic, got %v", errs)
					}
				} else if len(errs) != 0 {
					t.Fatalf("valid mapping rejected: %v", errs)
				}
			})
		}
	}
}

func TestExpressionWhitespacePreservesExpressionErrors(t *testing.T) {
	for _, value := range []string{"${{ ... }}", "${{ fromJSON( }}", "${{ 'unfinished }}"} {
		t.Run(value, func(t *testing.T) {
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    env: \"" + value + "\"\n    steps:\n      - run: echo ok\n"
			errs, err := linter.Lint("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) != 1 || errs[0].Kind != "expression" {
				t.Fatalf("want one expression syntax diagnostic, got %v", errs)
			}
		})
	}
}
