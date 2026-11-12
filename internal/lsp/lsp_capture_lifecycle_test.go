package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results"
)

func editorSymlink(tb testing.TB, target, alias string) {
	tb.Helper()
	if err := os.Symlink(target, alias); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			tb.Skipf("host symlink privilege unavailable: %v", err)
		}
		tb.Fatal(err)
	}
}

func TestEditorRealConflictingBuffersPreserveCallerAndOtherEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, directory := range []string{".git", "local", "alias"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path, alias := filepath.Join(root, "local", "action.yml"), filepath.Join(root, "alias", "action.yml")
	if err := os.WriteFile(path, []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	editorSymlink(t, path, alias)
	caller, good := filepath.Join(root, "workflow.yml"), filepath.Join(root, "good.yml")
	callerText := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./local}]}}\npermissions: {}\n"
	goodText := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo ::set-output name=result::good'}]}}\npermissions: {}\n"
	first := "name: first\ndescription: first\nruns: {using: composite, steps: []}\n"
	second := "name: second\ndescription: second\nruns: {using: composite, steps: []}\n"
	var previous string
	for _, reverse := range []bool{false, true} {
		a, b := first, second
		if reverse {
			a, b = b, a
		}
		analysis, err := analyzeDocuments(t.Context(), checking.Options{WorkingDir: root, SkipProjectConfig: true}, map[string]Document{path: {Text: a}, alias: {Text: b}, caller: {Text: callerText}, good: {Text: goodText}})
		if _, conflicted := errors.AsType[capture.ConflictError](err); !conflicted {
			t.Fatalf("real buffer aliases lost typed conflict: %v", err)
		}
		if previous != "" && previous != err.Error() {
			t.Fatalf("conflict depended on buffer order: %q vs %q", previous, err)
		}
		previous = err.Error()
		if _, exists := analysis.Sources[path]; exists {
			t.Fatal("real conflicted alias supplied editor text")
		}
		if _, exists := analysis.Sources[alias]; exists {
			t.Fatal("symlink conflicted alias supplied editor text")
		}
		if analysis.Sources[caller].Text != callerText || analysis.Sources[good].Text != goodText {
			t.Fatal("conflict discarded healthy caller or other source")
		}
		found := false
		for _, finding := range analysis.Diagnostics {
			if finding.Path == path || finding.Path == alias {
				t.Fatal("conflicted source published coordinates")
			}
			found = found || finding.Path == good && finding.Rule == "deprecated-commands"
		}
		if !found {
			t.Fatalf("healthy entry finding lost: %+v", analysis.Diagnostics)
		}
	}
}

func TestEditorMissingOnDiskEntryAndExcludedDependencyUseBuffers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow, dependency := filepath.Join(root, "workflow.yml"), filepath.Join(root, "virtual", "action.yml")
	text := "name: virtual\ndescription: virtual\nruns: {using: composite, steps: [{shell: bash, run: 'echo ::set-output name=result::virtual'}]}\n"
	overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte("files: {excludes: ['virtual/**']}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	analysis, err := analyzeDocuments(t.Context(), checking.Options{WorkingDir: root, SkipProjectConfig: true, ConfigOverlays: []repoconfig.Overlay{overlay}, ReadFile: func(path string) ([]byte, error) {
		if path == workflow || path == dependency {
			reads.Add(1)
		}
		return os.ReadFile(path)
	}}, map[string]Document{workflow: {Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./virtual}]}}\npermissions: {}\n"}, dependency: {Text: text}})
	if err != nil || len(analysis.Diagnostics) != 1 || analysis.Diagnostics[0].Path != dependency || analysis.Sources[dependency].Text != text || reads.Load() != 0 {
		t.Fatalf("virtual buffers fell back to disk: analysis=%+v error=%v reads=%d", analysis, err, reads.Load())
	}
	if analysis.Diagnostics[0].Reference == nil || analysis.Diagnostics[0].Reference.Revision != sourceid.Digest([]byte(text)) {
		t.Fatal("virtual dependency digest did not match its buffer")
	}
}

func TestEditorSymlinkDependencyAndClosingBufferUseCurrentCapture(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, directory := range []string{".git", "real"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path, alias := filepath.Join(root, "real", "action.yml"), filepath.Join(root, "alias", "action.yml")
	disk := "name: disk\ndescription: disk\nruns: {using: composite, steps: [{shell: bash, run: 'echo ::set-output name=result::disk'}]}\n"
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	editorSymlink(t, filepath.Dir(path), filepath.Dir(alias))
	workflow := filepath.Join(root, "workflow.yml")
	buffer := "name: buffer\ndescription: buffer\nruns: {using: composite, steps: [{shell: bash, run: echo fixed}]}\n"
	documents := map[string]Document{workflow: {Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./alias}]}}\npermissions: {}\n"}, path: {Version: 8, Text: buffer}}
	options := checking.Options{WorkingDir: root, SkipProjectConfig: true}
	first, err := analyzeDocuments(t.Context(), options, documents)
	if err != nil || len(first.Diagnostics) != 0 || first.Sources[alias].Text != buffer || first.Sources[path].Text != buffer {
		t.Fatalf("symlink dependency did not use real-path buffer: %+v %v", first, err)
	}
	delete(documents, path)
	second, err := analyzeDocuments(t.Context(), options, documents)
	if err != nil || len(second.Diagnostics) != 1 || second.Diagnostics[0].Rule != "deprecated-commands" || second.Sources[alias].Text != disk {
		t.Fatalf("closed buffer did not fall back to disk: %+v %v", second, err)
	}
	if second.Sources[alias].Revision == first.Sources[alias].Revision || second.Diagnostics[0].Reference == nil || second.Diagnostics[0].Reference.Revision != sourceid.Digest([]byte(disk)) {
		t.Fatal("closed overlay retained stale source identity")
	}
}

func TestEditorInvalidUTF8DependencyCannotSupplyRanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, directory := range []string{".git", "invalid"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "invalid", "action.yml")
	if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(root, "workflow.yml")
	analysis, err := analyzeDocuments(t.Context(), checking.Options{WorkingDir: root, SkipProjectConfig: true}, map[string]Document{workflow: {Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v6}, {uses: ./invalid}]}}\npermissions: {}\n"}})
	if _, exists := analysis.Sources[path]; exists {
		t.Fatal("invalid UTF-8 supplied editor source text")
	}
	for _, finding := range analysis.Diagnostics {
		if finding.Path == path {
			t.Fatalf("invalid UTF-8 fabricated a diagnostic range: %+v", finding)
		}
	}
	if _, unavailable := errors.AsType[*UnavailableSourcesError](err); !unavailable {
		t.Fatalf("missing invalid-source coordinates were not explicitly incomplete: analysis=%+v error=%v", analysis, err)
	}
}

type cancellingEditorAnalyzer struct{ started chan struct{} }

func (*cancellingEditorAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "editor-cancellation", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (a *cancellingEditorAnalyzer) CheckDocument(ctx context.Context, _ checkinputs.Input) ([]results.Diagnostic, error) {
	close(a.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCancelledEditorAnalysisPublishesNoCapturedText(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	started := make(chan struct{})
	type result struct {
		analysis Analysis
		err      error
	}
	done := make(chan result, 1)
	go func() {
		analysis, err := analyzeDocuments(ctx, checking.Options{WorkingDir: root, SkipProjectConfig: true, Analyzers: []checkinputs.Registration{{Checker: &cancellingEditorAnalyzer{started: started}}}}, map[string]Document{filepath.Join(root, "workflow.yml"): {Text: "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\npermissions: {}\n"}})
		done <- result{analysis, err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("analysis did not reach cancellable work")
	}
	cancel()
	select {
	case completed := <-done:
		if !errors.Is(completed.err, context.Canceled) || len(completed.analysis.Diagnostics) != 0 || len(completed.analysis.Sources) != 0 {
			t.Fatalf("cancelled generation exposed observations: %+v %v", completed.analysis, completed.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled analysis did not join")
	}
}
