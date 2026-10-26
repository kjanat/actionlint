package shellcheck_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type occurrenceGuest struct{ documentGuest }

func (g *occurrenceGuest) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	g.inputs = append(g.inputs, input)
	return nil, nil
}

type occurrenceObserver struct{ input analyzer.Input }

func (*occurrenceObserver) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "occurrence-observer", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations, analyzer.Execution}}
}

func (observer *occurrenceObserver) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	observer.input = input
	return nil, nil
}

func TestDocumentPluginUsesCanonicalOccurrences(t *testing.T) {
	root := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if output, err := workspace.GitCommand(t.Context(), git, root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture: %v: %s", err, output)
	}
	for name, content := range map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: 'echo nested # comment'}]}\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	guest, observer := &occurrenceGuest{}, &occurrenceObserver{}
	registration, err := (shellcheck.Options{Command: "shellcheck"}).Registration().WithAnalyzer(guest)
	if err != nil {
		t.Fatal(err)
	}
	const workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}, {uses: $/outer}, {uses: $/outer}]}}\n"
	filename := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(filename, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = application.Check(t.Context(), application.Options{WorkingDir: root, Tools: []tools.Registration{registration}, Analyzers: []analyzer.Registration{{Analyzer: observer}}}, application.Input{Kind: application.Stdin, Name: filename, Reader: strings.NewReader(workflow)})
	if err != nil {
		t.Fatal(err)
	}
	if len(guest.inputs) != 3 || len(observer.input.Scripts) != 1 || len(observer.input.Invoked) != 2 {
		t.Fatalf("missing executions: guest=%d root=%d invoked=%d", len(guest.inputs), len(observer.input.Scripts), len(observer.input.Invoked))
	}
	want := map[analyzer.OccurrenceID]analyzer.Script{}
	for _, scripts := range [][]analyzer.Script{observer.input.Scripts, observer.input.Invoked} {
		for _, script := range scripts {
			want[script.Occurrence.ID] = script
		}
	}
	seen := map[analyzer.OccurrenceID]bool{}
	for _, input := range guest.inputs {
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		if input.Reference != observer.input.Reference || input.Path != filename || input.Content != workflow || input.Kind != "workflow" || input.ParseStatus != observer.input.ParseStatus || !reflect.DeepEqual(input.CallGraph, observer.input.CallGraph) {
			t.Fatal("adapter rebuilt source header or graph")
		}
		if len(input.SelectedScripts) != 1 {
			t.Fatal("missing explicit selection")
		}
		id := input.SelectedScripts[0]
		if seen[id] {
			t.Fatal("duplicate occurrence")
		}
		seen[id] = true
		scripts := slices.Concat(input.Scripts, input.Invoked)
		if len(scripts) != 1 || !reflect.DeepEqual(scripts[0], want[id]) {
			t.Fatalf("adapter changed canonical script %s", id)
		}
		if len(input.Invoked) == 1 && (scripts[0].Occurrence.Depth != 2 || len(scripts[0].Occurrence.Ancestry) != 3 || scripts[0].Path == "") {
			t.Fatal("nested script lost ancestry or declaration path")
		}
	}
}
