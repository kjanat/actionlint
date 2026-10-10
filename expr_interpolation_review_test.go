package actionlint

import (
	"strings"
	"testing"
)

func TestInterpolationChecksContinueAfterSemanticErrors(t *testing.T) {
	value := "${{ unknown.value }} ${{ github.ref && '' || 'fallback' }}"
	for _, field := range []struct {
		name, workflow string
	}{
		{"workflow env", "on: push\nenv:\n  VALUE: " + value + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"},
		{"job env", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env:\n      VALUE: " + value + "\n    steps:\n      - run: echo ok\n"},
		{"step env", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          VALUE: " + value + "\n        run: echo ok\n"},
		{"step name", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - name: " + value + "\n        run: echo ok\n"},
		{"action input", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n        with:\n          path: " + value + "\n"},
		{"matrix string", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        value:\n          - " + value + "\n    steps:\n      - run: echo ok\n"},
	} {
		for _, levels := range []struct {
			expression, ternary string
			wantExpression      bool
			wantTernary         bool
		}{
			{"off", "on", false, true},
			{"on", "on", true, true},
			{"off", "warn", false, true},
			{"on", "off", true, false},
			{"off", "off", false, false},
		} {
			t.Run(field.name+"/"+levels.expression+"/"+levels.ternary, func(t *testing.T) {
				cfg, err := ParseConfig([]byte("lint: {rules: {correctness: {expression: " + levels.expression + ", unsound-ternary: " + levels.ternary + "}}}"))
				if err != nil {
					t.Fatal(err)
				}
				for _, ending := range []string{"\n", "\r\n"} {
					workflow := strings.ReplaceAll(field.workflow, "\n", ending)
					result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(workflow), Config: cfg}}})
					if err != nil {
						t.Fatal(err)
					}
					found := map[string]int{}
					for _, diagnostic := range result.Diagnostics {
						found[diagnostic.Rule]++
						if diagnostic.Rule != "unsound-ternary" {
							continue
						}
						before, _, foundOperand := strings.Cut(workflow, "'' ||")
						if !foundOperand {
							t.Fatal("test workflow is missing the falsy operand")
						}
						line := strings.Count(before, "\n") + 1
						column := len(before) - strings.LastIndex(before, "\n")
						if diagnostic.Start != (DiagnosticPosition{Line: line, Column: column}) {
							t.Errorf("later interpolation location: got %+v, want %d:%d", diagnostic.Start, line, column)
						}
						if levels.ternary == "warn" && diagnostic.Severity != "warning" {
							t.Errorf("independent warning level lost: %+v", diagnostic)
						}
					}
					if (found["expression"] != 0) != levels.wantExpression || (found["unsound-ternary"] != 0) != levels.wantTernary ||
						found["expression"] > 1 || found["unsound-ternary"] > 1 || len(found) > 2 {
						t.Fatalf("independent interpolation diagnostics lost: %+v", result.Diagnostics)
					}
				}
			})
		}
	}
}

func TestInterpolationSemanticErrorsRemainInvalid(t *testing.T) {
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ unknown.value }} ${{ github.ref && '' || 'fallback' }}",
			"${{ github.ref && '' || 'fallback' }} ${{ unknown.value }}",
			"${{ unknown.first }} ${{ unknown.second }} ${{ github.ref && '' || 'fallback' }}",
			"${{ missingFunction('}}') }} ${{ github.ref && '' || 'fallback' }}",
		} {
			rule := NewRuleExpression(nil, nil)
			types, valid := rule.checkExprsIn(source, &Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || len(types) != strings.Count(source, "${{") {
				t.Fatalf("must scan all parsed interpolations without treating their types as valid: %v, %+v", valid, types)
			}
			ternaries := 0
			for _, finding := range rule.Errs() {
				if finding.Kind == "unsound-ternary" {
					ternaries++
				}
			}
			if ternaries != 1 {
				t.Fatalf("later diagnostic missing: %+v", rule.Errs())
			}
			if ty := rule.checkOneExpression(&String{Value: source, Pos: &Pos{Line: 1, Col: 1}}, "value", "env"); ty != nil {
				t.Fatalf("invalid interpolation types escaped into type checking: %v", ty)
			}
		}
	}
}

func TestInterpolationParseErrorsStillStopScanning(t *testing.T) {
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ github.ref + }} ${{ github.ref && '' || 'fallback' }}",
			"${{ 'unterminated }} ${{ github.ref && '' || 'fallback' }}",
			"${{ unknown.value",
		} {
			rule := NewRuleExpression(nil, nil)
			types, valid := rule.checkExprsIn(source, &Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || types != nil || len(rule.Errs()) == 0 {
				t.Fatalf("unparsed interpolation must remain invalid: %v, %+v, %+v", valid, types, rule.Errs())
			}
			for _, finding := range rule.Errs() {
				if finding.Kind != "expression" {
					t.Fatalf("must not resume from an unparsed expression boundary: %+v", finding)
				}
			}
		}
	}
}
