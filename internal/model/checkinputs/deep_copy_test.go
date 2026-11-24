package checkinputs

import (
	"context"
	"testing"
	"testing/fstest"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func TestInputDetachedKeepsCapabilitiesAndIsolatesData(t *testing.T) {
	t.Parallel()
	registry, err := commands.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	environment := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Linux}, scripts.EnvironmentLayer{Names: []string{"ORIGINAL"}})
	calls := 0
	input := AnalyzerInput{
		Scripts:               []Script{{Spans: []sourcemap.Span{{End: 7}}, Runtime: &scripts.Execution{Shell: scripts.Shell{Name: "bash"}}, Environment: environment, Interpreters: registry}},
		Mappings:              []facts.Mapping{{Entries: []mappings.Declaration{{Name: "original"}}}},
		Files:                 []FileSystem{{Path: "/original", FS: fstest.MapFS{"file": &fstest.MapFile{Data: []byte("content")}}}},
		BuildShellScriptInput: func(context.Context, invocation.ID, *string) (*ShellScriptInput, error) { calls++; return nil, nil },
	}
	detached := input.Detached()
	detached.Scripts[0].Spans[0].End = 99
	detached.Scripts[0].Runtime.Shell.Name = "changed"
	detached.Scripts[0].Environment[0] = scripts.EnvironmentNames{}
	detached.Mappings[0].Entries[0].Name = "changed"
	detached.Files[0].Path = "/changed"
	if _, err := detached.BuildShellScriptInput(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	if input.Scripts[0].Spans[0].End != 7 || input.Scripts[0].Runtime.Shell.Name != "bash" || len(input.Scripts[0].Environment[0].Names()) != 1 || input.Mappings[0].Entries[0].Name != "original" || input.Files[0].Path != "/original" {
		t.Fatalf("detached input changed the source: %+v", input)
	}
	if detached.Scripts[0].Interpreters != registry || detached.Files[0].FS == nil || calls != 1 {
		t.Fatal("copy did not retain the supplied capabilities")
	}
}
