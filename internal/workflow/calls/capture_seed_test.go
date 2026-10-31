package calls

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/source"
)

func TestSeedUsesCanonicalRepositoryRootAndRechecksCapture(t *testing.T) {
	for _, symlinked := range []bool{false, true} {
		name := "relative-root"
		if symlinked {
			name = "symlink-root"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "repository")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			cacheRoot := "repository"
			if symlinked {
				cacheRoot = filepath.Join(base, "linked")
				if err := os.Symlink(root, cacheRoot); err != nil {
					if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
						t.Skipf("symlink privilege unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			path := filepath.Join(root, ".github", "workflows", "callee.yml")
			content := []byte("on:\n  workflow_call:\n    inputs:\n      message: {type: string, required: true}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n")
			var reads atomic.Int32
			store, err := capture.New(t.Context(), []capture.Overlay{{Path: path, Content: content, Coordinates: source.YAML}}, func(context.Context, string) ([]byte, error) {
				reads.Add(1)
				return nil, errors.New("seed unexpectedly read backing source")
			})
			if err != nil {
				t.Fatal(err)
			}
			capturefixture.CloseCaptures(t, store)
			captured, err := store.Read(t.Context(), path, source.YAML)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := documentparse.NewSourceSnapshots(store)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := registry.Document(captured, documentparse.Workflow(path, content))
			if err != nil {
				t.Fatal(err)
			}
			cache := NewCache(cacheRoot, base, nil)
			cache.SetSources(t.Context(), store, base)
			if err := cache.Seed(snapshot); err != nil {
				t.Fatal(err)
			}
			spec := "./.github/workflows/callee.yml"
			seeded, err, found := cache.Lookup(spec)
			if err != nil || !found || seeded == nil || !seeded.Inputs["message"].Required {
				t.Fatalf("workflow was not seeded: metadata=%+v error=%v found=%v", seeded, err, found)
			}
			loaded, err := cache.FindMetadata(spec)
			if err != nil || !cmp.Equal(loaded, seeded) || reads.Load() != 0 {
				t.Fatalf("seed fell back to a second source path: metadata=%+v error=%v reads=%d", loaded, err, reads.Load())
			}
			if err := store.Quarantine(captured, "conflicting declaration kinds: action, workflow"); err == nil {
				t.Fatal("quarantine did not report a conflict")
			}
			if metadata, err := cache.FindMetadata(spec); metadata != nil {
				t.Fatal("cached callee metadata outlived its capture")
			} else if _, ok := errors.AsType[capture.ConflictError](err); !ok {
				t.Fatalf("cached capture conflict was lost: %v", err)
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil || len(rows) != 1 || rows[0].Document != captured.Revision().Document() {
				t.Fatalf("identity-only repository root entered source table: rows=%+v error=%v", rows, err)
			}
		})
	}
}
