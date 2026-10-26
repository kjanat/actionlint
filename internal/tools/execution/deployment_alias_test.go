package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/source"
)

type invocationSourceAnalyzer struct {
	t     *testing.T
	paths map[source.Reference]string
}

func (a invocationSourceAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck"}
}

func (a invocationSourceAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.t.Helper()
	if len(input.Scripts) != 1 || input.Scripts[0].Invocation == nil {
		a.t.Fatal("nested invocation missing")
	}
	script := input.Scripts[0]
	path, exists := a.paths[script.Invocation.Reference]
	if !exists || script.Path != path {
		a.t.Fatalf("source path crossed invocation boundary: %+v", script)
	}
	return []diagnostic.Diagnostic{{Rule: "shellcheck", Message: "alias finding", Start: script.Start}}, nil
}

type deploymentAliasTool struct {
	t       *testing.T
	paths   []string
	content []byte
}

func (a deploymentAliasTool) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	a.t.Helper()
	if len(batch.Scripts) != len(a.paths) {
		a.t.Fatal("native adapter lost script occurrences")
	}
	var findings []tools.Finding
	for i, script := range batch.Scripts {
		if script.Path != a.paths[i] || string(script.Content) != string(a.content) || script.Scope == nil || script.Scope.ActionPath != filepath.Dir(a.paths[i]) {
			a.t.Fatalf("native adapter reused another deployment: %+v", script)
		}
		findings = append(findings, tools.Finding{Rule: "shellcheck", Message: "alias finding", Path: script.Path, Start: diagnostic.Position{Line: script.Value.Line, Column: script.Value.Col}, Script: i, Content: script.Content})
	}
	return findings, nil
}

func TestSharedSourceRetainsEachDeploymentDisplayPath(t *testing.T) {
	_, collection, snapshot, path := actionBatchFixture(t, false)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := snapshot.Content()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias.yml")
	if err := os.Symlink(path, alias); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	aliasDeployment, err := NewDeployment(snapshot, alias, false)
	if err != nil {
		t.Fatal(err)
	}
	graph, collection := nestedActionBatch(t, collection, snapshot, collection.Scopes[0].Deployment, aliasDeployment)
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	units := batch.Scripts()
	want := []string{path, path, alias, alias}
	if len(units) != len(want) || units[0].Reference != units[2].Reference || units[1].Reference != units[3].Reference || units[0].Scope.Context.ID() == units[2].Scope.Context.ID() {
		t.Fatal("fixture did not reuse declarations under distinct invocations")
	}
	byCall := map[source.Reference]string{}
	for i, unit := range units {
		if unit.Path != want[i] || string(unit.Content) != string(content) || &unit.Content[0] != &units[0].Content[0] || unit.Scope.Deployment.ActionPath() != filepath.Dir(want[i]) || unit.Scope.Paths.ActionPath != filepath.Dir(want[i]) {
			t.Fatalf("source cache erased per-invocation deployment: %+v", unit)
		}
		record, exists := graph.Script(unit.Occurrence)
		if !exists {
			t.Fatal("unregistered unit")
		}
		call, exists := graph.Call(record.Owner)
		if !exists {
			t.Fatal("unit lost call owner")
		}
		byCall[call.Site()] = want[i]
	}
	for name, adapter := range map[string]Analyzer{
		"fact":   FactAnalyzer{Analyzer: invocationSourceAnalyzer{t: t, paths: byCall}},
		"native": Tool{Runner: deploymentAliasTool{t: t, paths: want, content: content}},
	} {
		t.Run(name, func(t *testing.T) {
			findings, err := adapter.AnalyzeScripts(t.Context(), batch)
			if err != nil || len(findings) != len(want) {
				t.Fatalf("alias findings: %v %v", findings, err)
			}
			for i, finding := range findings {
				if finding.Path != want[i] || string(finding.Source) != string(content) {
					t.Fatalf("finding lost invocation display path: %+v", finding)
				}
			}
		})
	}
}
