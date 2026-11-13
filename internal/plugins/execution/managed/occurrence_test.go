package managed_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	scriptsbackend "actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type occurrenceGuest struct{ documentGuest }

func (g *occurrenceGuest) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	g.inputs = append(g.inputs, input)
	return nil, nil
}

type occurrenceObserver struct{ input checkinputs.Input }

func (*occurrenceObserver) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "occurrence-observer", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts, rulemeta.Invocations, rulemeta.Execution}}
}

func (observer *occurrenceObserver) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	observer.input = input
	return nil, nil
}

func emptyGitConfig(t *testing.T) string {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return global
}

func occurrenceRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	if output, err := gitclient.Command(t.Context(), git, root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture: %v: %s", err, output)
	}
	return root
}

func TestDocumentPluginUsesCanonicalOccurrences(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", emptyGitConfig(t))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := occurrenceRepository(t)
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
	registration, err := shellcheckRegistration(scriptsbackend.Selection{Command: "shellcheck"}).WithAnalyzer(guest)
	if err != nil {
		t.Fatal(err)
	}
	const workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}, {uses: $/outer}, {uses: $/outer}]}}\npermissions: {}\n"
	filename := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(filename, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = checking.Check(t.Context(), checking.Options{WorkingDir: root, Tools: []tools.Registration{registration}, Analyzers: []checkinputs.Registration{{Checker: observer}}}, inputmodel.Request{Kind: inputmodel.Stdin, Name: filename, Reader: strings.NewReader(workflow)})
	if err != nil {
		t.Fatal(err)
	}
	if len(guest.inputs) != 3 || len(observer.input.Scripts) != 1 || len(observer.input.Invoked) != 2 {
		t.Fatalf("missing executions: guest=%d root=%d invoked=%d", len(guest.inputs), len(observer.input.Scripts), len(observer.input.Invoked))
	}
	want := map[checkinputs.OccurrenceID]checkinputs.Script{}
	for _, scripts := range [][]checkinputs.Script{observer.input.Scripts, observer.input.Invoked} {
		for _, script := range scripts {
			want[script.Occurrence.ID] = script
		}
	}
	seen := map[checkinputs.OccurrenceID]bool{}
	for _, input := range guest.inputs {
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		if input.Reference != observer.input.Reference || input.Path != filename || input.Content != workflow || input.Kind != "workflow" || input.ParseStatus != observer.input.ParseStatus || input.CallGraph != observer.input.CallGraph {
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

type actionInputObserver struct{ inputs []checkinputs.Script }

func (observer *actionInputObserver) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	for _, script := range batch.Scripts {
		if script.Input != nil {
			observer.inputs = append(observer.inputs, script.Fact)
		}
	}
	return nil, nil
}

func TestDocumentPluginReceivesRunScriptsAndSkipsOtherToolInputs(t *testing.T) {
	t.Parallel()
	guest, observer := &occurrenceGuest{}, &actionInputObserver{}
	registration, err := shellcheckRegistration(scriptsbackend.Selection{Command: "shellcheck"}).WithAnalyzer(guest)
	if err != nil {
		t.Fatal(err)
	}
	route := tools.Registration{
		Descriptor:   rulemeta.Descriptor{Name: "input-observer", Category: "correctness", Recommended: true},
		ScriptInputs: []checkinputs.ScriptInput{{Action: "actions/github-script", Input: "script", Language: "javascript"}},
		Build:        func(tools.Context) (tools.Runner, error) { return observer, nil },
	}
	const workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo shell}, {shell: python, run: 'print(1)'}, {uses: actions/github-script@v7, with: {script: 'return 1'}}]}}\npermissions: {}\n"
	_, err = checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: []tools.Registration{registration, route}}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)})
	if err != nil {
		t.Fatal(err)
	}
	if len(observer.inputs) != 1 || observer.inputs[0].Occurrence.Kind != checkinputs.ActionInputScript {
		t.Fatalf("action-input route did not execute: %+v", observer.inputs)
	}
	if len(guest.inputs) != 2 {
		t.Fatalf("run calls=%d, want 2", len(guest.inputs))
	}
	for i, text := range []string{"echo shell", "print(1)"} {
		input := guest.inputs[i]
		if len(input.Scripts) != 1 || input.Scripts[0].Text != text || input.CallGraph.ScriptCount() != 3 {
			t.Fatalf("call %d contains the wrong script", i)
		}
		prepared, err := input.BuildShellScriptInput(t.Context(), input.Scripts[0].Occurrence.ID, nil)
		if err != nil || prepared == nil {
			t.Fatalf("call %d preparation=%+v error=%v", i, prepared, err)
		}
		if i == 1 && prepared.Dialect != nil {
			t.Fatal("host assigned a shell dialect to Python")
		}
	}
}

func TestDocumentPluginRetainsStandaloneActionEntry(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", emptyGitConfig(t))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := occurrenceRepository(t)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "action.yml"), []byte("name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const content = "name: entry\ndescription: entry\nruns: {using: composite, steps: [{shell: bash, run: echo root}, {uses: $/child}]}\n"
	path := filepath.Join(root, "action.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	guest, observer := &occurrenceGuest{}, &occurrenceObserver{}
	registration, err := shellcheckRegistration(scriptsbackend.Selection{Command: "shellcheck"}).WithAnalyzer(guest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = checking.Check(t.Context(), checking.Options{WorkingDir: root, Tools: []tools.Registration{registration}, Analyzers: []checkinputs.Registration{{Checker: observer}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(guest.inputs) != 2 || len(observer.input.Scripts) != 1 || len(observer.input.Invoked) != 1 {
		t.Fatal("standalone scripts lost")
	}
	for _, input := range guest.inputs {
		if input.Kind != "action" || input.Reference != observer.input.Reference || input.CallGraph.Entry().Kind != checkinputs.ActionEntry || input.CallGraph != observer.input.CallGraph {
			t.Fatal("standalone entry identity changed")
		}
	}
	if guest.inputs[0].Scripts[0].Occurrence.Depth != 0 || guest.inputs[1].Invoked[0].Occurrence.Depth != 1 {
		t.Fatal("standalone depth changed")
	}
}
