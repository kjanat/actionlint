package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestResultCaptureSharesRevisionAcrossRepositoryCaches(t *testing.T) {
	root, _ := executableFixture(t)
	const manifest = "name: shared\ndescription: capture\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo first\n"
	path := writeFixture(t, root, "shared/action.yml", manifest)
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/shared\n"
	var reads atomic.Int32
	collector := &invocationCollector{}
	result, err := application.Analyze(t.Context(), application.Options{
		WorkingDir: root,
		Analyzers:  []analyzer.Registration{{Analyzer: collector}},
		ReadFile: func(requested string) ([]byte, error) {
			if filepath.Clean(requested) != path {
				return os.ReadFile(requested)
			}
			if reads.Add(1) == 1 {
				return []byte(manifest), nil
			}
			return []byte("name: changed\ndescription: changed\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo second\n"), nil
		},
	}, []workspace.Source{
		{Path: "one.yml", InputPath: filepath.Join(root, "one.yml"), Content: []byte(workflow), Project: workspace.Identity(root)},
		{Path: "two.yml", InputPath: filepath.Join(root, "two.yml"), Content: []byte(workflow), Project: workspace.Identity(root + string(filepath.Separator) + ".")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || reads.Load() != 1 {
		t.Fatalf("result=%v, shared manifest reads=%d", result != nil, reads.Load())
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if len(collector.scripts) != 2 {
		t.Fatalf("invoked scripts=%d, want both entry observations", len(collector.scripts))
	}
	for _, observed := range collector.scripts {
		if observed.Text != "echo first" || observed.Reference != collector.scripts[0].Reference {
			t.Fatalf("entry observed a different captured revision: %+v", observed.Script)
		}
	}
}

func TestResultCaptureQuarantinesConflictsWithoutDiscardingOtherEntries(t *testing.T) {
	root, _ := executableFixture(t)
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"
	sources := []workspace.Source{
		{Path: "bad-one.yml", InputPath: filepath.Join(root, "shared.yml"), Content: []byte(workflow)},
		{Path: "bad-two.yml", InputPath: root + string(filepath.Separator) + "." + string(filepath.Separator) + "shared.yml", Content: []byte("name: conflicting\n" + workflow)},
		{Path: "good.yml", InputPath: filepath.Join(root, "good.yml"), Content: []byte(workflow)},
	}
	for _, reversed := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "reverse"}[reversed], func(t *testing.T) {
			ordered := slices.Clone(sources)
			if reversed {
				slices.Reverse(ordered)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, ordered)
			if err == nil || strings.Count(err.Error(), "source-revision-conflict") != 2 {
				t.Fatalf("both conflicting entries must report failure: %v", err)
			}
			if result == nil || len(result.Diagnostics) == 0 {
				t.Fatal("unrelated entry findings were discarded")
			}
			for _, finding := range result.Diagnostics {
				if finding.Path != "good.yml" {
					t.Fatalf("quarantined document published a finding: %+v", finding)
				}
			}
			if len(result.Documents) != 1 || result.Documents[0] == nil || result.Documents[0].DocumentPath() != "good.yml" {
				t.Fatalf("quarantined entry published an outline: %+v", result.Documents)
			}
		})
	}
}
