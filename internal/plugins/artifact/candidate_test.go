package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type candidateReader func([]byte) (int, error)

func (r candidateReader) Read(p []byte) (int, error) { return r(p) }

type candidateTransport struct{ requests int }

func (r *candidateTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.requests++
	return nil, errors.New("unexpected candidate network request")
}

// Rejected imports must leave both existing cache entries and absent entries alone.
func TestCandidateImportPreservesCacheOnFailure(t *testing.T) {
	data := module(`{"id":"candidate"}`)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	readFailure := errors.New("source read failed")
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"digest mismatch", "size", "cancel", "read"} {
			t.Run(fmt.Sprintf("existing=%t/%s", existing, failure), func(t *testing.T) {
				cache := t.TempDir()
				options := Options{CacheDir: cache}
				if existing {
					if err := Import(t.Context(), bytes.NewReader(data), digest, options); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var input io.Reader = bytes.NewReader(data)
				want := ""
				switch failure {
				case "digest mismatch":
					input = bytes.NewReader(module(`{"id":"different"}`))
					want = "SHA256 mismatch"
				case "size":
					options.MaxBytes = int64(len(data) - 1)
					want = "byte limit"
				case "cancel":
					reader := bytes.NewReader(data)
					input = candidateReader(func(p []byte) (int, error) {
						cancel()
						return reader.Read(p)
					})
				case "read":
					input = candidateReader(func([]byte) (int, error) { return 0, readFailure })
				}
				err := Import(ctx, input, digest, options)
				if err == nil || want != "" && !strings.Contains(err.Error(), want) || failure == "cancel" && !errors.Is(err, context.Canceled) || failure == "read" && !errors.Is(err, readFailure) {
					t.Fatalf("wrong import error: %v", err)
				}
				entries, err := os.ReadDir(cache)
				if err != nil {
					t.Fatal(err)
				}
				if !existing {
					if len(entries) != 0 {
						t.Fatalf("rejected import wrote cache files: %v", entries)
					}
					return
				}
				got, err := os.ReadFile(filepath.Join(cache, digest))
				if err != nil || !bytes.Equal(got, data) || len(entries) != 1 {
					t.Fatalf("rejected import changed cache: files=%v bytes=%x err=%v", entries, got, err)
				}
			})
		}
	}
}

// Invalid import requests fail before consuming their source.
func TestCandidateImportRejectsInvalidRequest(t *testing.T) {
	input := candidateReader(func([]byte) (int, error) {
		t.Fatal("invalid request consumed candidate bytes")
		return 0, io.EOF
	})
	for _, digest := range []string{"", "../outside", strings.Repeat("a", 63), strings.Repeat("a", 66), strings.Repeat("z", 64)} {
		if err := Import(t.Context(), input, digest, Options{CacheDir: t.TempDir()}); err == nil {
			t.Fatalf("accepted invalid digest %q", digest)
		}
	}
	if err := Import(t.Context(), input, strings.Repeat("a", 64), Options{}); err == nil {
		t.Fatal("accepted missing cache directory")
	}
}

// Candidate execution uses only imported bytes and rechecks their digest.
func TestCandidateCacheResolution(t *testing.T) {
	transport := &candidateTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	raw := module(`{"id":"candidate"}`)
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("gzip=%t", packed), func(t *testing.T) {
			data := raw
			if packed {
				data = compressed(t, raw)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			source := "sha256://" + digest
			options := Options{CacheDir: t.TempDir(), MaxBytes: int64(len(data))}
			if err := Import(t.Context(), bytes.NewReader(data), digest, options); err != nil {
				t.Fatal(err)
			}
			if err := RequireRemotePin(source); err != nil {
				t.Fatalf("cache source rejected for execution: %v", err)
			}
			got, err := Resolve(t.Context(), source, options)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes, raw) || got.Info.SHA256 != digest || got.Info.Size != len(data) || got.Info.Kind != Core || string(got.Info.Metadata) != `{"id":"candidate"}` {
				t.Fatalf("wrong imported artifact: %+v", got)
			}
			path := filepath.Join(options.CacheDir, digest)
			if err := os.WriteFile(path, module(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Resolve(t.Context(), source, options); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
				t.Fatalf("corrupt candidate accepted: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if _, err := Resolve(t.Context(), source, options); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing cache error: %v", err)
			}
			if _, err := Resolve(t.Context(), source, Options{}); err == nil {
				t.Fatal("cache source accepted without cache directory")
			}
		})
	}
	if transport.requests != 0 {
		t.Fatalf("candidate execution fetched %d times", transport.requests)
	}
}

// A cache locator cannot carry another location or a second digest.
func TestCandidateCacheRejectsInvalidURI(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	for _, source := range []string{
		"sha256://", "sha256://../outside", "sha256://" + strings.Repeat("a", 63),
		"sha256://" + strings.Repeat("z", 64), "sha256://" + strings.ToUpper(digest),
		"sha256://user@" + digest, "sha256://" + digest + ":443", "sha256://" + digest + "/file",
		"sha256://" + digest + "?", "sha256://" + digest + "?url=https://example.com",
		"sha256://" + digest + "#sha256=" + digest,
	} {
		if _, _, err := NormalizeSource(source, t.TempDir()); err == nil {
			t.Errorf("accepted cache locator %q", source)
		}
	}
}
