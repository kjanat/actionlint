package scripts_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

type stdoutStreamReference struct {
	Name     string `json:"name"`
	Shell    string `json:"shell"`
	Script   string `json:"script"`
	Stdout   string `json:"stdout"`
	Exit     int    `json:"exit"`
	Findings int    `json:"findings"`
}

func stdoutStreamReferences(t *testing.T) []stdoutStreamReference {
	t.Helper()
	data, err := os.ReadFile("testdata/stdout-stream/reference.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var cases []stdoutStreamReference
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var fixture stdoutStreamReference
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 18 {
		t.Fatalf("got %d reference cases, want 18", len(cases))
	}
	return cases
}

func TestStdoutStreamReferenceRecorded(t *testing.T) {
	t.Parallel()
	registry := interpreters.Builtin()
	for _, tc := range stdoutStreamReferences(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			paths := scripts.LiteralStdoutPaths(tc.Script, registry.Analyze(tc.Shell, false), registry)
			if len(paths) != 1 {
				t.Fatalf("got %d modeled paths, want one reference path", len(paths))
			}
			var actual []string
			var state commands.WorkflowCommandState
			findings := 0
			for _, line := range paths[0] {
				actual = append(actual, line.Text)
				if state.WorkflowCommandProblem(line.Text) != "" {
					findings++
				}
			}
			var expected []string
			scanner := bufio.NewScanner(strings.NewReader(tc.Stdout))
			for scanner.Scan() {
				expected = append(expected, scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(actual, expected) || findings != tc.Findings {
				t.Fatalf("lines=%q findings=%d; Bash lines=%q expected findings=%d", actual, findings, expected, tc.Findings)
			}
		})
	}
}

// Live comparison is opt-in so normal tests do not require Bash or a WSL setup.
func TestStdoutStreamReferenceLive(t *testing.T) {
	t.Parallel()
	bash := os.Getenv("ACTIONLINT_BASH_REFERENCE")
	if bash == "" {
		t.Skip("set ACTIONLINT_BASH_REFERENCE to a Bash executable for live replay")
	}
	for _, tc := range stdoutStreamReferences(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			args := []string{"--noprofile", "--norc", "-e", "-o", "pipefail", "-c", tc.Script}
			if tc.Shell == "bash +e {0}" {
				args = []string{"--noprofile", "--norc", "+e", "-c", tc.Script}
			}
			command := exec.CommandContext(t.Context(), bash, args...)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			exit := 0
			if err != nil {
				if status, ok := errors.AsType[*exec.ExitError](err); ok {
					exit = status.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if stdout.String() != tc.Stdout || exit != tc.Exit || stderr.Len() != 0 {
				t.Fatalf("stdout=%q exit=%d stderr=%q; want stdout=%q exit=%d", stdout.String(), exit, stderr.String(), tc.Stdout, tc.Exit)
			}
		})
	}
}
