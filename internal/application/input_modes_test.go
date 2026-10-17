package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestCheckInputModesPreserveConfigurationAndOrder(t *testing.T) {
	root := t.TempDir()
	writeGitRepository(t, root)
	writeFixture(t, root, ".github/actionlint.yaml", "config-variables: []\n")
	content := strings.Replace(badWorkflow, "missing.value", "vars.UNKNOWN", 1)
	first := writeFixture(t, root, ".github/workflows/a.yml", content)
	second := writeFixture(t, root, ".github/workflows/b.yml", content)
	relFirst, _ := filepath.Rel(root, first)
	relSecond, _ := filepath.Rel(root, second)
	for _, tc := range []struct {
		name            string
		input           application.Input
		paths, selected []string
	}{
		{"repository", application.Input{Kind: application.Repository}, []string{relFirst, relSecond}, []string{first, second}},
		{"files", application.Input{Kind: application.Files, Paths: []string{second, first}}, []string{relSecond, relFirst}, []string{second, first}},
		{"file", application.Input{Kind: application.Files, Paths: []string{first}}, []string{relFirst}, []string{first}},
		{"stdin", application.Input{Kind: application.Stdin, Name: first, Reader: strings.NewReader(content)}, []string{first}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var selected []string
			result, err := application.Check(t.Context(), application.Options{WorkingDir: root, OnFilesSelected: func(paths []string) {
				calls++
				selected = slices.Clone(paths)
				if len(paths) > 0 {
					paths[0] = "callback-must-not-change-input.yml"
				}
			}}, tc.input)
			if err != nil || result == nil {
				t.Fatalf("check: %v", err)
			}
			if !slices.Equal(selected, tc.selected) || (calls == 0) != (tc.selected == nil) || calls > 1 {
				t.Fatalf("selection callback: %d calls %v, want %v", calls, selected, tc.selected)
			}
			if len(result.Diagnostics) != len(tc.paths) {
				t.Fatalf("expected one config-dependent diagnostic per workflow: %+v", result.Diagnostics)
			}
			for i, path := range tc.paths {
				if d := result.Diagnostics[i]; d.Path != path || d.Rule != "expression-reference" {
					t.Fatalf("wrong diagnostic order or path: %+v", result.Diagnostics)
				}
			}
		})
	}
	t.Run("directory snapshots", func(t *testing.T) {
		session, err := workspace.New(workspace.Options{WorkingDir: root})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := session.Directory(filepath.Dir(first), nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, selected.Sources)
		if err != nil || len(result.Diagnostics) != 2 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		for i, path := range []string{relFirst, relSecond} {
			if d := result.Diagnostics[i]; d.Path != path || d.Rule != "expression-reference" {
				t.Fatalf("wrong snapshot finding: %+v", d)
			}
		}
	})
}
func TestCheckCancelledEmptySelection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	result, err := application.Check(ctx, application.Options{OnFilesSelected: func([]string) { calls++ }}, application.Input{Kind: application.Files})
	if !errors.Is(err, context.Canceled) || result != nil || calls != 0 {
		t.Fatalf("cancelled invocation performed work: result=%+v err=%v callbacks=%d", result, err, calls)
	}
}
func TestCheckVerboseStdinLogOrder(t *testing.T) {
	var log strings.Builder
	_, err := application.Check(t.Context(), application.Options{WorkingDir: t.TempDir(), Verbose: true, LogSelection: true, LogWriter: &log}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(goodWorkflow)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(log.String(), "verbose: Reading the input from stdin\nverbose: Linting <stdin>\n") {
		t.Fatalf("wrong selection/analysis log order: %s", &log)
	}
}
