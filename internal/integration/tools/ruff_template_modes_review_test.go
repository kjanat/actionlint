package tools_test

import (
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestRuffStatementTemplatesPreserveFindings(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	config, err := repoconfig.Parse([]byte("tools: {ruff: {select: [F, B018]}}"))
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
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}})
				if err != nil {
					t.Fatal(err)
				}
				var codes []string
				var ruff []diagmodel.Diagnostic
				interpolations := 0
				for _, diagnostic := range result.Diagnostics {
					switch diagnostic.Rule {
					case "ruff":
						codes = append(codes, diagnostic.Code)
						ruff = append(ruff, diagnostic)
					case "script-interpolation":
						if diagnostic.Code != "unknown-script-value" {
							t.Fatal(diagnostic)
						}
						interpolations++
					default:
						t.Fatalf("unexpected finding: %+v", diagnostic)
					}
				}
				wantInterpolations := 0
				if strings.Contains(tc.statement, "${{") {
					wantInterpolations = 1
				}
				if interpolations != wantInterpolations {
					t.Fatalf("want %d interpolation findings, got %d", wantInterpolations, interpolations)
				}
				offset := strings.Count(tc.statement, "\n")
				if tc.opaqueStatement {
					if !slices.Equal(codes, []string{"F821"}) {
						t.Fatalf("opaque statement affected other steps: %+v", result.Diagnostics)
					}
					finding := ruff[0]
					if finding.Start != (diagmodel.Position{Line: 12 + offset, Column: 20}) || finding.End != (diagmodel.Position{Line: 12 + offset, Column: 34}) || !strings.Contains(finding.Message, "second_missing") {
						t.Fatalf("independent step finding moved: %+v", finding)
					}
					return
				}
				if !slices.Equal(codes, []string{"B018", "F821"}) ||
					ruff[0].Start != (diagmodel.Position{Line: 9 + offset, Column: 11}) || ruff[0].End != (diagmodel.Position{Line: 9 + offset, Column: 13}) ||
					ruff[1].Start != (diagmodel.Position{Line: 10 + offset, Column: 17}) || ruff[1].End != (diagmodel.Position{Line: 10 + offset, Column: 24}) {
					t.Fatalf("statement template changed independent findings: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestRuffConversionTemplateSkipsOnlyItsScript(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	config, err := repoconfig.Parse([]byte("tools: {ruff: {target-version: py314}}"))
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
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, templateDiagnosticLines(script, 8)...)
			if len(ruff) != 1 || ruff[0].Code != "F821" || ruff[0].Start.Line != 10+strings.Count(script, "\n") {
				t.Fatalf("conversion template %q changed independent script findings: %+v", script, result.Diagnostics)
			}
		}
	}
}

func TestRuffAPINonDiagnosticModes(t *testing.T) {
	command := testutil.RequireTool(t, "ruff")
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	for _, flag := range []string{"--help", "-h", "--watch", "-w", "--add-noqa", "--add-noqa=reviewed", "--add-ignore", "--add-ignore=reviewed"} {
		_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: []string{flag}}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
		if err == nil || !strings.Contains(err.Error(), "non-diagnostic mode") {
			t.Fatalf("mode not rejected: flag=%s error=%v", flag, err)
		}
	}
}
