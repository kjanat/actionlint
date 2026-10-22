package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/rule"
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
	registration := Process(rule.Descriptor{Name: "catalogue"}, shells, executable, &processconfig.Options{
		Executable: &executable, LauncherArguments: []string{"-test.run=^TestShellSelectorProcessHelper$"},
		Environment: []string{"ACTIONLINT_SELECTOR_HELPER=1"},
	})
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

func TestMaskExpressionsUsesExpressionBoundaries(t *testing.T) {
	for _, expression := range []string{"${{ '}}' }}", "${{ 'é' }}", "${{\n github.ref\n}}", "${{ 'it''s }}' }}"} {
		source := "prefix " + expression + " suffix"
		got := maskExpressions(source)
		if !strings.HasPrefix(got, "prefix ") || !strings.HasSuffix(got, " suffix") || utf8.RuneCountInString(got) != utf8.RuneCountInString(source) || strings.Count(got, "\n") != strings.Count(source, "\n") {
			t.Fatalf("mask %q: %q", source, got)
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(got, "prefix "), " suffix")
		if strings.Trim(middle, "_\n\r") != "" {
			t.Fatalf("expression partially exposed: %q", got)
		}
	}
	if got := maskExpressions("${{ unfinished"); got != "${{ unfinished" {
		t.Fatalf("invalid expression: %q", got)
	}
}
