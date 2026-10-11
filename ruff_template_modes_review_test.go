package actionlint

import (
	"slices"
	"strings"
	"testing"
)

func TestRuffStatementTemplatesPreserveFindings(t *testing.T) {
	command := ruffForTest(t)
	config, err := ParseConfig([]byte("tools: {ruff: {select: [F, B018]}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, statement string
		opaqueStatement bool
	}{
		{"static statement", "print(1)", false},
		{"opaque statement", "${{ 'print(1)' }}", true},
		{"parenthesized value", "(${{ 'print(1)' }})", false},
		{"nested parenthesized value", "((${{ 'print(1)' }}))", false},
		{"multiline opaque statement", "${{\n 'print(1)'\n}}", true},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				script := tc.statement + "\n42\nprint(missing)"
				source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          ` + strings.ReplaceAll(script, "\n", "\n          ") + "\n"
				if tc.opaqueStatement {
					// An opaque statement skips only its containing script.
					source += `      - shell: python
        run: print(second_missing)
`
				}
				result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}}})
				if err != nil {
					t.Fatal(err)
				}
				var codes []string
				for _, diagnostic := range result.Diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				offset := strings.Count(tc.statement, "\n")
				if tc.opaqueStatement {
					if !slices.Equal(codes, []string{"F821"}) {
						t.Fatalf("opaque statement affected other steps: %+v", result.Diagnostics)
					}
					finding := result.Diagnostics[0]
					if finding.Start != (DiagnosticPosition{Line: 12 + offset, Column: 20}) || finding.End != (DiagnosticPosition{Line: 12 + offset, Column: 34}) || !strings.Contains(finding.Message, "second_missing") {
						t.Fatalf("independent step finding moved: %+v", finding)
					}
					return
				}
				if !slices.Equal(codes, []string{"B018", "F821"}) ||
					result.Diagnostics[0].Start != (DiagnosticPosition{Line: 9 + offset, Column: 11}) || result.Diagnostics[0].End != (DiagnosticPosition{Line: 9 + offset, Column: 13}) ||
					result.Diagnostics[1].Start != (DiagnosticPosition{Line: 10 + offset, Column: 17}) || result.Diagnostics[1].End != (DiagnosticPosition{Line: 10 + offset, Column: 24}) {
					t.Fatalf("statement template changed independent findings: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestRuffConversionTemplateSkipsOnlyItsScript(t *testing.T) {
	command := ruffForTest(t)
	config, err := ParseConfig([]byte("tools: {ruff: {target-version: py314}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`print(t"{1!${{ 'r' }}}")`,
		`print(rt"{1!${{ 'r' }}}")`,
		`print(tr"{1!${{ 'r' }}}")`,
		`print(f"{1:'>5} {2!${{ 'r' }}}")`,
		`print(f"{1:{2!${{ 'r' }}}}")`,
		`print(f"{ {'key': 1} !${{ 'r' }}}")`,
		`print(f"{(lambda: 1)()!${{ 'r' }}}")`,
		"print(f\"\"\"{1 # ' comment\n!${{ 'r' }}}\"\"\")",
		`print(f"{value!${{ 'r' }}}")`,
		`print(f"{"value"!${{ 'r' }}}")`,
		`print(f"{f"{value}"!${{ 'r' }}}")`,
		`print(f"{r"value"!${{ 'r' }}}")`,
		`print(f"{"""value"""!${{ 'r' }}}")`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          ` + strings.ReplaceAll(script, "\n", "\n          ") + `
      - shell: python
        run: print(missing)
`
			result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 10+strings.Count(script, "\n") {
				t.Fatalf("conversion template %q changed independent script findings: %+v", script, result.Diagnostics)
			}
		}
	}
}

func TestRuffAPINonDiagnosticModes(t *testing.T) {
	command := ruffForTest(t)
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	for _, flag := range []string{"--help", "-h", "--watch", "-w", "--add-noqa", "--add-noqa=reviewed", "--add-ignore", "--add-ignore=reviewed"} {
		_, err := Analyze(t.Context(), AnalysisRequest{RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: []string{flag}}, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
		if err == nil || !strings.Contains(err.Error(), "non-diagnostic mode") {
			t.Fatalf("mode not rejected: flag=%s error=%v", flag, err)
		}
	}
}
