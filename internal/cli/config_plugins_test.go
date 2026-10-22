package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestConfigInspectionLoadsPluginRules(t *testing.T) {
	commandTestRepo(t)
	for _, setting := range []string{
		"ignores: [{rule: no-sudo, job: build}]\n",
		"lint: {rules: {disable: [no-sudo]}}\n",
		"policy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n",
	} {
		if err := os.WriteFile(".github/actionlint.yaml", []byte(setting), 0o600); err != nil {
			t.Fatal(err)
		}
		checked := testRunCommand(pluginWorkflow, "check", "--plugin", pluginFixture("no-sudo"), "-")
		if checked.Status > 1 || checked.Stderr != "" {
			t.Fatalf("check rejected plugin configuration %q: %+v", setting, checked)
		}
		for _, command := range [][]string{{"config", "show"}, {"config", "validate"}, {"doctor"}} {
			args := append(append([]string{}, command...), "--json", "--plugin", pluginFixture("no-sudo"))
			got := testRunCommand("", args...)
			if got.Status != 0 || got.Stderr != "" {
				t.Errorf("%v rejected registered rule in %q: %+v", command, setting, got)
			}
			missing := testRunCommand("", append(append([]string{}, command...), "--json")...)
			if missing.Status == 0 || !strings.Contains(missing.Stdout+missing.Stderr, "no-sudo") {
				t.Errorf("%v accepted unregistered rule: %+v", command, missing)
			}
		}
	}
	if err := os.WriteFile(".github/actionlint.yaml", []byte("lint: {rules: {disable: [no-sudoo]}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"config", "show"}, {"config", "validate"}, {"doctor"}} {
		got := testRunCommand("", append(command, "--json", "--plugin", pluginFixture("no-sudo"))...)
		if got.Status == 0 || !strings.Contains(got.Stdout+got.Stderr, "no-sudoo") {
			t.Errorf("%v accepted misspelled plugin rule: %+v", command, got)
		}
	}
}

func TestConfigInspectionPluginFailures(t *testing.T) {
	for _, command := range [][]string{{"config", "show"}, {"config", "validate"}, {"doctor"}} {
		got := testRunCommand("", append(command, "--no-config", "--plugin", pluginFixture("missing"))...)
		if got.Status == 0 || !strings.Contains(got.Stdout+got.Stderr, "missing.wasm") {
			t.Errorf("%v ignored plugin load failure: %+v", command, got)
		}
	}
}

func TestConfigInspectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, request := range []commandRequest{
		configShowRequest{}, configValidateRequest{}, doctorRequest{},
	} {
		if _, err := request.run(ctx, Command{Stdout: io.Discard}); !errors.Is(err, context.Canceled) {
			t.Errorf("%T ignored cancellation: %v", request, err)
		}
	}
}
