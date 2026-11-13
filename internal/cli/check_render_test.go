package cli

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRenderColor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode      render.Color
		terminal  bool
		noColor   string
		term      string
		wantColor bool
	}{
		{mode: render.ColorAlways, noColor: "1", term: "dumb", wantColor: true},
		{mode: render.ColorNever, terminal: true, term: "xterm"},
		{mode: render.ColorAuto, terminal: true, term: "xterm", wantColor: true},
		{mode: render.ColorAuto, term: "xterm"},
		{mode: render.ColorAuto, terminal: true, noColor: "1", term: "xterm"},
		{mode: render.ColorAuto, terminal: true, noColor: "0", term: "xterm"},
		{mode: render.ColorAuto, terminal: true, term: "dumb"},
		{mode: render.ColorAuto, terminal: true, wantColor: true},
	} {
		env := map[string]string{"NO_COLOR": tc.noColor, "TERM": tc.term}
		if got := renderColor(tc.mode, tc.terminal, func(name string) string { return env[name] }); got != tc.wantColor {
			t.Errorf("%+v: color=%t", tc, got)
		}
	}
}

func TestCheckColorLeavesOtherRenderersPlain(t *testing.T) {
	t.Parallel()
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	finding := render.FindingOf(results.Diagnostic{
		Rule: "expression-reference", Message: "unknown variable", Path: "ci.yml",
		Start: sourcepos.Position{Line: 1, Column: 3}, End: sourcepos.Position{Line: 1, Column: 6},
	}, []byte("  bad"))
	stop := make(chan struct{})
	failures := make(chan error, 4)
	var renders sync.WaitGroup
	for _, format := range []render.Format{render.FormatText, render.FormatPretty, render.FormatText, render.FormatPretty} {
		renders.Go(func() {
			renderer, err := render.New(format, "", false, buildinfo.Version, nil)
			if err != nil {
				failures <- err
				return
			}
			for {
				select {
				case <-stop:
					return
				default:
				}
				var out strings.Builder
				if err := renderer.Render(&out, render.Result{Findings: []render.Finding{finding}}); err != nil {
					failures <- err
					return
				}
				if ansi.MatchString(out.String()) {
					failures <- fmt.Errorf("%s: renderer without a color mode wrote color: %q", format, out.String())
					return
				}
			}
		})
	}
	for _, args := range [][]string{
		{"actionlint", "--summary=false", "--no-tool=shellcheck", "--no-tool=ruff", "--color", "-"},
		{"actionlint", "check", "--no-config", "--summary=false", "--no-tool=shellcheck", "--no-tool=ruff", "--color=always", "--hyperlinks=never", "-"},
	} {
		var out, stderr strings.Builder
		cmd := Command{Stdin: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"), Stdout: &out, Stderr: &stderr}
		if code := cmd.MainContext(t.Context(), args); code != 1 || !ansi.MatchString(out.String()) {
			t.Errorf("%q: exit=%d stdout=%q stderr=%s", args, code, out.String(), &stderr)
		}
	}
	close(stop)
	renders.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
