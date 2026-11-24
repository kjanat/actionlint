package artifacts

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"golang.org/x/sync/errgroup"
)

func TestConcurrentArtifactPublicationAndReads(t *testing.T) {
	t.Parallel()
	for _, cold := range []bool{false, true} {
		t.Run(fmt.Sprintf("cold=%t", cold), func(t *testing.T) {
			t.Parallel()
			store := Store{Dir: t.TempDir()}
			data := bytes.Repeat([]byte("verified artifact\n"), 256)
			digest := digestOf(data)
			if !cold {
				if err := store.Publish(t.Context(), digest, data); err != nil {
					t.Fatal(err)
				}
			}
			group, ctx := errgroup.WithContext(t.Context())
			start := make(chan struct{})
			for worker := range 8 {
				group.Go(func() error {
					<-start
					for range 100 {
						if worker%2 == 0 {
							if err := cacheFile(ctx, filepath.Join(store.Dir, digest), data); err != nil {
								return err
							}
						} else {
							actual, err := store.Read(ctx, digest, int64(len(data)))
							if cold && errors.Is(err, os.ErrNotExist) {
								continue
							}
							if err != nil {
								return err
							}
							if !bytes.Equal(actual, data) {
								return errors.New("concurrent artifact read returned different bytes")
							}
						}
					}
					return nil
				})
			}
			close(start)
			if err := group.Wait(); err != nil {
				t.Fatal(err)
			}
			actual, err := store.Read(t.Context(), digest, int64(len(data)))
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatalf("published artifact=%q error=%v", actual, err)
			}
		})
	}
}

func TestPublishRepairsCorruptArtifacts(t *testing.T) {
	t.Parallel()
	for _, corrupt := range []string{"bad", "invalid", "this corrupt file exceeds the expected artifact size"} {
		t.Run(corrupt, func(t *testing.T) {
			t.Parallel()
			store := Store{Dir: t.TempDir()}
			data := []byte("correct")
			digest := digestOf(data)
			if err := os.WriteFile(filepath.Join(store.Dir, digest), []byte(corrupt), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := store.Publish(t.Context(), digest, data); err != nil {
				t.Fatal(err)
			}
			actual, err := store.Read(t.Context(), digest, int64(len(data)))
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatalf("repaired artifact=%q error=%v", actual, err)
			}
		})
	}
}

func TestPublishArtifactInLongCachePath(t *testing.T) {
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), strings.Repeat("a", 150), strings.Repeat("b", 150))}
	data := []byte("long cache path")
	digest := digestOf(data)
	if err := store.Publish(t.Context(), digest, data); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Read(t.Context(), digest, int64(len(data)))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("long cache path artifact=%q error=%v", actual, err)
	}
}

func TestOCIReferenceRequiresDigest(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct{ source, want string }{
		{"oci://ghcr.io/kjanat/tool:v1", "must pin a digest"},
		{"oci://ghcr.io/kjanat/tool", "must pin a digest"},
		{"oci://ghcr.io/kjanat/Tool@sha256:" + digest, "lowercase"},
		{"oci://ghcr.io/kjanat/tool@sha512:" + digest, "sha256"},
		{"oci://user:secret@ghcr.io/kjanat/tool@sha256:" + digest, "invalid OCI reference"},
		{"oci://ghcr.io", "invalid OCI reference"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			if err := RequireRemotePin(tc.source); err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v", err)
			}
			if _, err := Resolve(t.Context(), tc.source, Options{}); err == nil {
				t.Fatal("resolved")
			}
		})
	}
	source := "oci://GHCR.io/kjanat/tool@sha256:" + digest
	if normalized, local, err := pluginreference.NormalizeSource(source, t.TempDir()); err != nil || local != "" || normalized != "oci://ghcr.io/kjanat/tool@sha256:"+digest {
		t.Fatalf("NormalizeSource = %q, %q, %v", normalized, local, err)
	}
	tagged, err := pluginreference.ParseOCITag("oci://ghcr.io/kjanat/tool:v1")
	if err != nil || tagged.Tag() != "v1" || tagged.Digest() != "" {
		t.Fatalf("ParseOCITag = %+v, %v", tagged, err)
	}
	if pinned, err := tagged.WithDigest(digest); err != nil || pinned.String() != "oci://ghcr.io/kjanat/tool@sha256:"+digest {
		t.Fatalf("WithDigest = %s, %v", pinned, err)
	}
}

func TestStoreMaterialize(t *testing.T) {
	t.Parallel()
	store := Store{Dir: t.TempDir()}
	data := []byte("#!/bin/sh\necho provider\n")
	digest := digestOf(data)
	if err := store.Publish(t.Context(), digest, data); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(t.Context(), digestOf([]byte("other")), data); err == nil {
		t.Fatal("published bytes under another digest")
	}
	path, err := store.Materialize(t.Context(), digest, "provider", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(store.Dir, "exec", digest, "provider")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode())
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if again, err := store.Materialize(t.Context(), digest, "provider", 1<<20); err != nil || again != path {
		t.Fatalf("Materialize = %s, %v", again, err)
	}
	if copied, err := os.ReadFile(path); err != nil || !bytes.Equal(copied, data) {
		t.Fatalf("copy not rewritten: %q, %v", copied, err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, digest), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(t.Context(), digest, "provider", 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt blob materialized: %v", err)
	}
	for _, name := range []string{"", ".", "..", "dir/provider", `dir\provider`, "c:provider"} {
		if _, err := store.Materialize(t.Context(), digest, name, 1<<20); err == nil {
			t.Fatalf("filename %q accepted", name)
		}
	}
}

func digestOf(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
