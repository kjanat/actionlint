package cli_test

import (
	"strings"
	"testing"
)

func TestGCCOutputSelection(t *testing.T) {
	for _, prefix := range [][]string{nil, {"check"}} {
		for _, flag := range []string{"-o", "--output", "--output-format"} {
			args := append(append([]string{}, prefix...), "--norc", flag, "gcc", "-")
			got := testRunCommand(commandBadWorkflow, args...)
			if got.Status != 1 || !strings.HasPrefix(got.Stdout, "<stdin>:6:24: error: undefined variable") ||
				!strings.HasSuffix(got.Stdout, " [unknown-variable]\n") || strings.Count(got.Stdout, "\n") != 1 || strings.Contains(got.Stdout, "\x1b") {
				t.Fatalf("gcc %v: %+v", args, got)
			}
			mixed := testRunCommand(commandBadWorkflow, append(args[:len(args)-1], "-o", "pretty", "-")...)
			if mixed.Status != 1 || !strings.HasPrefix(mixed.Stdout, got.Stdout+"error[expression-reference]") {
				t.Fatalf("repeated output lost gcc or pretty: %+v", mixed)
			}
		}
	}
	t.Setenv("ACTIONLINT_OUTPUT_FORMAT", "gcc")
	got := testRunCommand(commandBadWorkflow, "--no-config", "-")
	if got.Status != 1 || !strings.HasPrefix(got.Stdout, "<stdin>:6:24: error:") {
		t.Fatalf("gcc environment format: %+v", got)
	}
	got = testRunCommand(strings.Replace(commandGoodWorkflow, "echo ok", "echo ok # ignore script-interpolation", 1), "--no-config", "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, ": note: inline suppression of") || !strings.HasSuffix(got.Stdout, " [unused-suppression]\n") {
		t.Fatalf("informational finding is not a compiler note: %+v", got)
	}
}
