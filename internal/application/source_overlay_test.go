package application

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"

	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/source"
)

func TestFileEntryUsesBufferThroughAlternateCanonicalAlias(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "buffer.yml")
	entryAlias := filepath.Join(root, "entry.yml")
	disk := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo disk}]}}\n"
	unsaved := "on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: 'echo ::set-output name=result::unsaved'}]}}\n"
	if err := os.WriteFile(realPath, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, entryAlias); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	var reads atomic.Int32
	result, err := Check(t.Context(), Options{WorkingDir: root, SkipProjectConfig: true, SourceOverlays: []SourceOverlay{{Path: realPath, Content: []byte(unsaved)}}, ReadFile: func(path string) ([]byte, error) {
		if path == entryAlias || path == realPath {
			reads.Add(1)
		}
		return os.ReadFile(path)
	}}, Input{Kind: Files, Paths: []string{entryAlias}})
	if err != nil || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "deprecated-commands" {
		t.Fatalf("disk selection conflicted with its open canonical source: result=%+v error=%v", result, err)
	}
	finding := result.Diagnostics[0]
	if finding.Reference == nil || finding.Reference.Revision != sourceid.Digest([]byte(unsaved)) || reads.Load() != 1 {
		t.Fatalf("selected source was reread or used stale bytes: finding=%+v reads=%d", finding, reads.Load())
	}
	for _, path := range []string{entryAlias, realPath} {
		captured, exists := result.CapturedSources[path]
		if !exists || string(captured.Content) != unsaved || captured.Coordinates != source.YAML || !captured.CoordinatesAvailable || captured.Revision.Digest() != finding.Reference.Revision {
			t.Fatalf("sealed alias lost capture provenance: %q %+v", path, captured)
		}
	}
	result.CapturedSources[realPath].Content[0] = '!'
	if string(result.CapturedSources[entryAlias].Content) != unsaved || string(result.Sources[0].Content) != unsaved {
		t.Fatal("mutable result projection changed another capture view")
	}
}

func TestPreparedExplicitBytesStillConflictWithBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yml")
	content := []byte("on: push\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo explicit}]}}\n")
	result, err := Analyze(t.Context(), Options{WorkingDir: root, SourceOverlays: []SourceOverlay{{Path: path, Content: []byte("on: push\njobs: {}\n")}}}, []workspace.Source{{Path: path, Content: content}})
	if _, conflicted := errors.AsType[capture.ConflictError](err); !conflicted {
		t.Fatalf("explicit prepared bytes were silently overridden: %v", err)
	}
	if result == nil || len(result.Diagnostics) != 0 || len(result.CapturedSources) != 0 {
		t.Fatalf("explicit conflicting source was publishable: %+v", result)
	}
}
