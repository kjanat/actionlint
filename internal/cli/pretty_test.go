package cli

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestPrettyCheckCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("NO_COLOR", "1")
	for _, mode := range []string{"never", "always"} {
		var out, stderr strings.Builder
		cmd := Command{
			Stdin:  strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"),
			Stdout: &out, Stderr: &stderr,
		}
		exit := cmd.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "--output-format=pretty", "--diagnostic-width=60", "--color=" + mode, "--summary=false", "-"})
		if exit != 1 {
			t.Fatalf("exit=%d stdout=%s stderr=%s", exit, &out, &stderr)
		}
		if !strings.Contains(out.String(), "error[expression-reference]") || !strings.Contains(out.String(), "  --> <stdin>:6:") {
			t.Fatalf("missing diagnostic: %s", &out)
		}
		if strings.Contains(out.String(), "\x1b[") != (mode == "always") {
			t.Fatalf("color=%s: %q", mode, out.String())
		}
	}
}

func TestPrettyOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		bad  bool
	}{
		{"default width", []string{"--output-format=pretty"}, false},
		{"default format", nil, false},
		{"default format custom width", []string{"--diagnostic-width=60"}, false},
		{"custom width", []string{"--output-format=pretty", "--diagnostic-width=60"}, false},
		{"too small", []string{"--output-format=pretty", "--diagnostic-width=1"}, true},
		{"too large", []string{"--output-format=pretty", "--diagnostic-width=401"}, true},
		{"wrong format", []string{"--output-format=json", "--diagnostic-width=60"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := newCommandApp(t.Context(), &Command{})
			if _, _, err := app.parseRoot(tc.args); err != nil {
				t.Fatal(err)
			}
			err := app.prepareInvocation()
			if (err != nil) != tc.bad {
				t.Fatalf("prepare: %v", err)
			}
			if tc.bad {
				return
			}
			if len(app.inv.Render.Formats) != 1 || app.inv.Render.Formats[0] != outputformat.Pretty {
				t.Fatal("format not propagated")
			}
			if _, err := newResultRenderers(app.inv.Render); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDependencyFindingsFlag(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"compact", "full", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			app := newCommandApp(t.Context(), &Command{})
			if _, _, err := app.parseRoot([]string{"--dependency-findings=" + mode}); err != nil {
				t.Fatal(err)
			}
			err := app.prepareInvocation()
			if mode == "invalid" {
				if err == nil || !strings.Contains(err.Error(), "--dependency-findings") {
					t.Fatalf("invalid mode accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			renderer, err := newResultRenderer(app.inv.Render, outputformat.Pretty)
			if err != nil {
				t.Fatal(err)
			}
			finding := results.Publish(results.Diagnostic{
				Origin: results.DependencyOrigin, Rule: "shellcheck", Severity: severity.Info,
				Path: "remote/action.yml", Message: "dependency detail",
			}, results.DiagnosticSource{})
			var out strings.Builder
			if err := renderer.Render(&out, output.RenderedReport{Findings: []results.PublishedDiagnostic{finding}}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "dependency detail") != (mode == "full") {
				t.Fatalf("%s output: %s", mode, out.String())
			}
		})
	}
}
