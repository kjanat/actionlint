package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const commandHelper = "CHECK_README_COMMAND_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(commandHelper) == "mise" {
		runMiseHelper()
		return
	}
	os.Exit(m.Run())
}

func runMiseHelper() {
	args := os.Args[1:]
	file, err := os.OpenFile(os.Getenv("CHECK_README_MISE_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := json.NewEncoder(file).Encode(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(args) > 0 && os.Getenv("CHECK_README_MISE_FAIL") == args[0] {
		fmt.Fprintln(os.Stderr, "requested tool resolution failed")
		os.Exit(2)
	}
	if len(args) == 2 && args[0] == "install" {
		os.Exit(0)
	}
	if len(args) == 4 && args[0] == "which" && args[1] == "actionlint" && args[2] == "--tool" {
		if output, ok := os.LookupEnv("CHECK_README_MISE_WHICH"); ok {
			fmt.Fprintln(os.Stdout, output)
		} else {
			fmt.Fprintln(os.Stdout, miseHelperBinary(args[3]))
		}
		os.Exit(0)
	}
	if len(args) < 6 || args[0] != "exec" || args[1] != "--no-deps" || args[3] != "--" || args[4] != miseHelperBinary(args[2]) {
		fmt.Fprintln(os.Stderr, "README probes must use the resolved release and skip project dependencies")
		os.Exit(2)
	}
	if args[len(args)-1] == "-version" {
		fmt.Fprintln(os.Stdout, "actionlint.kjanat.dev 1.17.0")
		os.Exit(0)
	}
	fmt.Fprintf(os.Stdout, "%s/%s:10:14: unknown label [runner-label]\n", fixtureDir, workflow)
	os.Exit(1)
}

func miseHelperBinary(tool string) string {
	name := "fork-release"
	if tool == upstreamTool+"@latest" {
		name = "upstream-release"
	}
	return filepath.Join(filepath.Dir(os.Getenv("CHECK_README_MISE_CALLS")), name, "actionlint")
}

func miseGeneratorFixture(t *testing.T) (generator, string) {
	t.Helper()
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	tool := "mise"
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(filepath.Join(root, tool), content, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "calls.jsonl")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(commandHelper, "mise")
	t.Setenv("CHECK_README_MISE_CALLS", capture)
	g := generator{ctx: t.Context(), root: root, log: log.New(io.Discard, "", 0)}
	return g, capture
}

func TestReleasedToolsSkipProjectDependencyPreparation(t *testing.T) {
	g, capture := miseGeneratorFixture(t)
	if _, _, err := g.lint(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.measureUpstream(); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(captured)), "\n")
	want := [][]string{
		{"install", forkTool + "@latest"},
		{"which", "actionlint", "--tool", forkTool + "@latest"},
		{"exec", "--no-deps", forkTool + "@latest", "--", miseHelperBinary(forkTool + "@latest"), "-version"},
		{"exec", "--no-deps", forkTool + "@latest", "--", miseHelperBinary(forkTool + "@latest"), "-no-color", "-config-file", filepath.Join(fixtureDir, config), filepath.Join(fixtureDir, workflow)},
		{"install", upstreamTool + "@latest"},
		{"which", "actionlint", "--tool", upstreamTool + "@latest"},
		{"exec", "--no-deps", upstreamTool + "@latest", "--", miseHelperBinary(upstreamTool + "@latest"), "-version"},
		{"exec", "--no-deps", upstreamTool + "@latest", "--", miseHelperBinary(upstreamTool + "@latest"), "-no-color", filepath.Join(fixtureDir, workflow)},
	}
	if len(lines) != len(want) {
		t.Fatalf("captured %d invocations, want %d: %s", len(lines), len(want), captured)
	}
	for i, line := range lines {
		var got []string
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want[i]) {
			t.Fatalf("invocation %d: got %q, want %q", i, got, want[i])
		}
	}
}

func TestReleasedCommandRejectsInvalidResolution(t *testing.T) {
	for _, output := range []string{"", "actionlint", "relative/actionlint", "/release/actionlint\n/other/actionlint"} {
		t.Run(output, func(t *testing.T) {
			g, capture := miseGeneratorFixture(t)
			t.Setenv("CHECK_README_MISE_WHICH", output)
			if _, err := g.releasedCommand(forkTool); err == nil || !strings.Contains(err.Error(), "invalid executable path") {
				t.Fatalf("invalid resolution accepted: %q, %v", output, err)
			}
			calls, err := os.ReadFile(capture)
			if err != nil || strings.Contains(string(calls), `"exec"`) {
				t.Fatalf("invalid resolution executed: %s, %v", calls, err)
			}
		})
	}
	for _, phase := range []string{"install", "which"} {
		t.Run(phase+" failure", func(t *testing.T) {
			g, capture := miseGeneratorFixture(t)
			t.Setenv("CHECK_README_MISE_FAIL", phase)
			if _, err := g.releasedCommand(forkTool); err == nil || !strings.Contains(err.Error(), "requested tool resolution failed") {
				t.Fatalf("failed resolution continued: %v", err)
			}
			calls, err := os.ReadFile(capture)
			if err != nil || strings.Contains(string(calls), `"exec"`) {
				t.Fatalf("failed resolution executed: %s, %v", calls, err)
			}
		})
	}
}

func TestNormalizeDiagnostics(t *testing.T) {
	const want = "demo-workflow.yaml:10:14: unknown label [runner-label]\n" +
		"demo-workflow.yaml:16:9: unexpected key [syntax-check]\n"
	for _, prefix := range []string{"docs/screenshots/", `docs\screenshots\`} {
		t.Run(prefix, func(t *testing.T) {
			input := strings.ReplaceAll(want, workflow, prefix+workflow)
			input = strings.ReplaceAll(input, "\n", "\r\n")
			got := normalizeDiagnostics(input)
			if got != want {
				t.Fatalf("normalized diagnostics = %q, want %q", got, want)
			}
			if count := len(diagnostic.FindAllString(got, -1)); count != 2 {
				t.Fatalf("counted %d diagnostics, want 2", count)
			}
		})
	}
}

func TestGeneratorCommandExitOne(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		allowExitOne bool
		wantOutput   string
		wantError    string
	}{
		{
			name:         "actionlint diagnostics",
			mode:         "diagnostic",
			allowExitOne: true,
			wantOutput:   "diagnostic\n",
		},
		{
			name:      "version probe",
			mode:      "diagnostic",
			wantError: "exit status 1",
		},
		{
			name:         "mise failure",
			mode:         "failure",
			allowExitOne: true,
			wantError:    "could not resolve tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(commandHelper, tt.mode)
			g := generator{ctx: context.Background()}
			out, err := g.command(tt.allowExitOne, os.Args[0], "-test.run=^TestCommandHelper$")
			if string(out) != tt.wantOutput {
				t.Fatalf("output was %q but wanted %q", out, tt.wantOutput)
			}
			if tt.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error was %v but wanted it to contain %q", err, tt.wantError)
			}
		})
	}
}

func TestCommandHelper(t *testing.T) {
	switch os.Getenv(commandHelper) {
	case "":
		return
	case "diagnostic":
		fmt.Fprintln(os.Stdout, "diagnostic")
	case "failure":
		fmt.Fprintln(os.Stderr, "could not resolve tool")
	default:
		t.Fatalf("unexpected helper mode %q", os.Getenv(commandHelper))
	}
	os.Exit(1)
}
