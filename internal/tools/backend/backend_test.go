package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/rule"
)

type selectorBackend struct{ calls atomic.Int64 }

func (b *selectorBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.calls.Add(1)
	return nil, nil
}

func TestLargeShellCatalogueIsAnImmutableRegistration(t *testing.T) {
	shells := make([]string, 10000)
	for i := range shells {
		shells[i] = fmt.Sprintf("shell-%d", i)
	}
	shells[len(shells)-1] = "python"
	backend := &selectorBackend{}
	registration, err := Register(rule.Descriptor{Name: "catalogue"}, shells, backend)
	if err != nil {
		t.Fatal(err)
	}
	shells[0], shells[len(shells)-1] = "bash", "bash"
	for range 2 {
		runner, err := registration.Build(tools.Context{})
		if err != nil {
			t.Fatal(err)
		}
		for _, shell := range []string{"shell-0", "python", `C:\Tools\PYTHON.exe -u`, "bash", "not-listed"} {
			if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Execution: tools.Execution{Shell: shell}}}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := backend.calls.Load(); got != 6 {
		t.Fatalf("catalogue selected %d scripts, want 6", got)
	}
}

func TestProcessShellCatalogueIsCapturedBeforeDocumentBuilds(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shells := []string{"python"}
	registration, err := RegisterProvider(Provider{
		Manifest: envelopeManifest("catalogue", shells),
		Selection: Selection{Process: &processconfig.Options{
			Executable: &executable, LauncherArguments: []string{"-test.run=^TestShellSelectorProcessHelper$"},
			Environment: []string{"ACTIONLINT_SELECTOR_HELPER=1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	shells[0] = "bash"
	processes := analyzerhost.NewProcessPool(t.Context(), 1)
	defer processes.Wait()
	for range 2 {
		runner, err := registration.Build(tools.Context{Processes: processes})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Execution: tools.Execution{Shell: "bash"}}}}); err != nil {
			t.Fatalf("caller changed registered catalogue: %v", err)
		}
	}
}

// TestRequiresExecutionFollowsShellsAndResources checks decision b4 as
// amended: a provider resolves each run step's shell and directory when its
// manifest declares selectors.shells or capabilities.resources, and not when
// it selects action inputs only.
func TestRequiresExecutionFollowsShellsAndResources(t *testing.T) {
	inputs := []analyzer.ScriptInput{{Action: "owner/repository", Input: "source", Language: "python"}}
	for _, tc := range []struct {
		name      string
		shells    []string
		resources []provider.Resource
		want      bool
	}{
		{"inputs only", nil, nil, false},
		{"shells", []string{"python"}, nil, true},
		{"resources", nil, []provider.Resource{provider.ResourceWorkspace, provider.ResourceDirectory}, true},
	} {
		manifest := envelopeManifest("execution", tc.shells)
		manifest.Selectors.Inputs = inputs
		manifest.Capabilities.Resources = tc.resources
		registration, err := RegisterProvider(Provider{Manifest: manifest, Selection: Selection{Command: "execution"}})
		if err != nil {
			t.Fatal(err)
		}
		if registration.RequiresExecution != tc.want {
			t.Fatalf("%s: RequiresExecution = %t, want %t", tc.name, registration.RequiresExecution, tc.want)
		}
	}
}

// envelopeManifest describes a process provider exchanging the JSON check
// request and result envelope with its engine.
func envelopeManifest(name string, shells []string) provider.Manifest {
	return provider.Manifest{
		SchemaVersion: provider.SchemaVersion, ID: "example.com/" + name, Version: "1", Role: provider.RoleScript,
		Rule: rule.Descriptor{Name: name}, Scope: provider.ScopeSource, Transport: provider.TransportProcess,
		Selectors: provider.Selectors{Shells: shells},
		Process: &provider.ProcessSpec{
			Entry: name, Stdin: provider.StdinRequest,
			Config: provider.ConfigRendering{Render: provider.RenderJSONStdin},
			Output: provider.Output{Format: provider.FormatEnvelope},
		},
	}
}

func TestShellSelectorProcessHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_SELECTOR_HELPER") != "1" {
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		OK     bool     `json:"ok"`
		Errors []string `json:"errors"`
	}{false, []string{"mutated shell was selected"}})
	os.Exit(0)
}

func BenchmarkShellCatalogueSelection(b *testing.B) {
	for _, count := range []int{16, 10000} {
		b.Run(fmt.Sprintf("shells=%d", count), func(b *testing.B) {
			shells := make([]string, count)
			for i := range shells {
				shells[i] = fmt.Sprintf("shell-%d", i)
			}
			registration, err := Register(rule.Descriptor{Name: "catalogue"}, shells, &selectorBackend{})
			if err != nil {
				b.Fatal(err)
			}
			runner, err := registration.Build(tools.Context{})
			if err != nil {
				b.Fatal(err)
			}
			batch := tools.Batch{Scripts: make([]tools.Script, 100)}
			for i := range batch.Scripts {
				batch.Scripts[i].Execution.Shell = "not-listed"
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := runner.Analyze(b.Context(), batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
