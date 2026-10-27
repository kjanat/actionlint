package cli

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/render"
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
		exit := cmd.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output-format=pretty", "--diagnostic-width=60", "--color=" + mode, "--summary=false", "-"})
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
	for _, tc := range []struct {
		name string
		args []string
		bad  bool
	}{
		{"default width", []string{"--output-format=pretty"}, false},
		{"custom width", []string{"--output-format=pretty", "--diagnostic-width=60"}, false},
		{"too small", []string{"--output-format=pretty", "--diagnostic-width=1"}, true},
		{"too large", []string{"--output-format=pretty", "--diagnostic-width=401"}, true},
		{"wrong format", []string{"--output-format=json", "--diagnostic-width=60"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCommandApp(&Command{})
			if _, _, _, err := app.parseRoot(tc.args); err != nil {
				t.Fatal(err)
			}
			err := app.prepareInvocation()
			if (err != nil) != tc.bad {
				t.Fatalf("prepare: %v", err)
			}
			if tc.bad {
				return
			}
			if app.inv.Render.Format != render.FormatPretty {
				t.Fatal("format not propagated")
			}
			if _, err := newResultRenderer(app.inv.Render); err != nil {
				t.Fatal(err)
			}
		})
	}
}
