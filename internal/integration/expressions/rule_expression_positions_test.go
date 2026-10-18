package expressions_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestWorkflowExpressionScalarPositions(t *testing.T) {
	for _, scalar := range []string{
		`"é\t${{ missing }}"`,
		`'é ''quote'' ${{ missing }}'`,
		"prefix\n      ${{ missing }}",
		">-\n      é prefix\n      ${{ missing }}",
		"|-\n      é prefix\n      ${{ missing }}",
		"\"é\\n${{ github.actor_id }} ${{ missing }}\"",
		"${{ github.actor &&\n      missing }}",
		`"${{ \u006dissing }}"`,
	} {
		for _, field := range []string{"env", "run"} {
			t.Run(field+"/"+scalar, func(t *testing.T) {
				src := "on: push\nenv:\n  VALUE: " + scalar + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				if field == "run" {
					src = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + strings.ReplaceAll(scalar, "\n", "\n    ") + "\n"
				}
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
				if err != nil {
					t.Fatal(err)
				}
				token := "missing"
				if strings.Contains(scalar, `\u006dissing`) {
					token = `\u006dissing`
				}
				offset := strings.Index(src, token)
				line := strings.Count(src[:offset], "\n") + 1
				column := utf8.RuneCountInString(src[strings.LastIndexByte(src[:offset], '\n')+1:offset]) + 1
				if len(result.Diagnostics) != 1 {
					t.Fatalf("got %+v", result.Diagnostics)
				}
				d := result.Diagnostics[0]
				if d.Rule != "expression-reference" || d.Start.Line != line || d.Start.Column != column {
					t.Fatalf("want %d:%d, got %+v", line, column, d)
				}
				if d.End.Line != line || d.End.Column != column+len(token) {
					t.Fatalf("wrong token end: %+v", d)
				}
			})
		}
	}
}

func TestWorkflowConditionScalarPositions(t *testing.T) {
	for _, scalar := range []string{
		`"github.actor == 'é' && missing"`,
		"github.actor &&\n      missing",
		">-\n      github.actor &&\n      missing",
		"|-\n      github.actor &&\n      missing",
		`"github.actor == 'é' &&\n missing"`,
	} {
		t.Run(scalar, func(t *testing.T) {
			src := "on: push\njobs:\n  test:\n    if: " + scalar + "\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
			if err != nil {
				t.Fatal(err)
			}
			offset := strings.Index(src, "missing")
			line := strings.Count(src[:offset], "\n") + 1
			column := utf8.RuneCountInString(src[strings.LastIndexByte(src[:offset], '\n')+1:offset]) + 1
			if len(result.Diagnostics) != 1 {
				t.Fatalf("got %+v", result.Diagnostics)
			}
			d := result.Diagnostics[0]
			if d.Start.Line != line || d.Start.Column != column || d.End.Line != line || d.End.Column != column+len("missing") {
				t.Fatalf("want %d:%d, got %+v", line, column, d)
			}
		})
	}
}

func TestWorkflowExpressionEOFPosition(t *testing.T) {
	for _, field := range []string{
		"    if: |\n      github.actor &&\n",
		"    if: >\n      github.actor &&\n",
		"    if: \"github.actor &&\"\n",
	} {
		src := "on: push\njobs:\n  test:\n" + field + "    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("got %+v", result.Diagnostics)
		}
		d := result.Diagnostics[0]
		if d.End != d.Start {
			t.Fatalf("EOF must be zero-width: %+v", d)
		}
		line := strings.Split(src, "\n")[d.Start.Line-1]
		if d.Start.Column > utf8.RuneCountInString(line)+1 {
			t.Fatalf("EOF exceeds source: %+v", d)
		}
	}
}

func TestWorkflowExpressionSourceScopeAndAliases(t *testing.T) {
	src := `on: push
env:
  CONDITION: &condition >-
    github.actor &&
    missingCondition
  FIRST: "${{ missingFirst && missingSecond }}"
  SECOND: >-
    ${{ missingThird }}
jobs:
  test:
    if: *condition
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 4 {
		t.Fatalf("got %+v", result.Diagnostics)
	}
	for _, token := range []string{"missingCondition", "missingFirst", "missingSecond", "missingThird"} {
		offset := strings.Index(src, token)
		line := strings.Count(src[:offset], "\n") + 1
		column := utf8.RuneCountInString(src[strings.LastIndexByte(src[:offset], '\n')+1:offset]) + 1
		found := false
		for _, d := range result.Diagnostics {
			if strings.Contains(d.Message, `"`+token+`"`) {
				found = true
				if d.Start.Line != line || d.Start.Column != column || d.End.Line != line || d.End.Column != column+len(token) {
					t.Fatalf("%s want %d:%d, got %+v", token, line, column, d)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s diagnostic: %+v", token, result.Diagnostics)
		}
	}
}

func TestWorkflowExpressionDottedReferenceRanges(t *testing.T) {
	for _, scalar := range []string{
		`${{ github.missing }}`,
		`"${{ github.\u006dissing }}"`,
		">-\n      ${{ github.\n      missing }}",
		"|-\n      ${{ github.\n      missing }}",
		"${{ github.\n      missing }}",
		`${{ github . missing }}`,
	} {
		t.Run(scalar, func(t *testing.T) {
			src := "on: push\nenv:\n  VALUE: " + scalar + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("got %+v", result.Diagnostics)
			}
			start := strings.Index(src, "github")
			end := strings.Index(src, "issing") + len("issing")
			position := func(offset int) (int, int) {
				return strings.Count(src[:offset], "\n") + 1, utf8.RuneCountInString(src[strings.LastIndexByte(src[:offset], '\n')+1:offset]) + 1
			}
			startLine, startCol := position(start)
			endLine, endCol := position(end)
			d := result.Diagnostics[0]
			if d.Start.Line != startLine || d.Start.Column != startCol || d.End.Line != endLine || d.End.Column != endCol {
				t.Fatalf("want %d:%d-%d:%d, got %+v", startLine, startCol, endLine, endCol, d)
			}
		})
	}
}
