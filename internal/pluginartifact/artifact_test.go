package pluginartifact

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func custom(name string, value []byte) []byte {
	payload := binary.AppendUvarint(nil, uint64(len(name)))
	payload = append(payload, name...)
	payload = append(payload, value...)
	section := binary.AppendUvarint([]byte{0}, uint64(len(payload)))
	return append(section, payload...)
}
func module(metadata string) []byte {
	data := []byte("\x00asm\x01\x00\x00\x00")
	if metadata != "" {
		data = append(data, custom(MetadataSection, []byte(metadata))...)
	}
	return data
}
func compressed(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestSniff(t *testing.T) {
	nested := append([]byte("\x00asm\x0d\x00\x01\x00"), 1)
	child := module(`{"id":"child"}`)
	nested = binary.AppendUvarint(nested, uint64(len(child)))
	nested = append(nested, child...)
	kind, metadata, err := Sniff(nested)
	if err != nil || kind != Component || metadata != nil {
		t.Fatalf("nested metadata leaked: %s %s %v", kind, metadata, err)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"header", []byte("not wasm")},
		{"version", []byte("\x00asm\x02\x00\x00\x00")},
		{"truncated length", append(module(""), 0, 128)},
		{"overflow", append(module(""), 0, 255, 255, 255, 255, 31)},
		{"truncated section", append(module(""), 0, 127)},
		{"bad name", append(module(""), 0, 2, 1, 255)},
		{"duplicate section", append(module(`{}`), custom(MetadataSection, []byte(`{}`))...)},
		{"duplicate JSON key", module(`{"id":"a","id":"b"}`)},
		{"duplicate nested key", module(`{"engine":{"name":"a","name":"b"}}`)},
		{"null", module(`null`)},
		{"array", module(`[]`)},
		{"invalid UTF8 JSON", module("{\"id\":\"\xff\"}")},
		{"oversize metadata", module(`{"padding":"` + strings.Repeat("x", maxMetadataBytes) + `"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Sniff(tc.data); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
	if !SameMetadata([]byte(`{"b":2,"a":1}`), []byte("{\n\"a\":1,\"b\":2}")) {
		t.Fatal("property order changed comparison")
	}
	if SameMetadata([]byte(`{"n":9007199254740992}`), []byte(`{"n":9007199254740993}`)) {
		t.Fatal("large integers rounded")
	}
}
func TestRemoteAndCache(t *testing.T) {
	raw := module(`{"id":"test"}`)
	for _, gzip := range []bool{false, true} {
		t.Run(strconv.FormatBool(gzip), func(t *testing.T) {
			data := raw
			if gzip {
				data = compressed(t, raw)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("transparent encoding enabled")
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			pin := fmt.Sprintf("%x", sha256.Sum256(data))
			location := server.URL + "/plugin?token=secret#sha256=" + pin
			options := Options{CacheDir: t.TempDir()}
			artifact, err := Resolve(t.Context(), location, options)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(artifact.Bytes, raw) || artifact.Info.SHA256 != pin || strings.Contains(artifact.Info.Source, "secret") {
				t.Fatalf("artifact=%+v", artifact.Info)
			}
			if _, err := Resolve(t.Context(), location, options); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests=%d", requests.Load())
			}
			for range 2 {
				if _, err := Resolve(t.Context(), server.URL, options); err != nil {
					t.Fatal(err)
				}
			}
			if requests.Load() != 3 {
				t.Fatal("unpinned URL reused without fetching")
			}
			server.Close()
			if _, err := Resolve(t.Context(), location, options); err != nil {
				t.Fatalf("verified cache not usable offline: %v", err)
			}
			if err := os.WriteFile(filepath.Join(options.CacheDir, pin), module(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Resolve(t.Context(), location, options); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
				t.Fatalf("corrupt cache: %v", err)
			}
		})
	}
}
func TestRemoteFailures(t *testing.T) {
	raw := module(`{"padding":"` + strings.Repeat("x", 2000) + `"}`)
	packed := compressed(t, raw)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/loop":
			http.Redirect(w, r, "/loop?token=secret", http.StatusFound)
		case "/slow":
			<-r.Context().Done()
		case "/gzip":
			_, _ = w.Write(packed)
		default:
			_, _ = w.Write(raw)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		path    string
		options Options
		want    string
	}{
		{"/missing?token=secret", Options{}, "HTTP 404"},
		{"/loop", Options{}, "redirect rejected"},
		{"/slow", Options{Timeout: 20 * time.Millisecond}, "deadline"},
		{"/", Options{MaxBytes: 100}, "limit"},
		{"/gzip", Options{MaxBytes: 200}, "limit"},
		{"/#sha256=" + strings.Repeat("0", 64), Options{}, "SHA256 mismatch"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			_, err := Resolve(t.Context(), server.URL+tc.path, tc.options)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Resolve(ctx, server.URL, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLocalURI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space # plugin.wasm")
	if err := os.WriteFile(path, module(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	for _, source := range []string{path, uri.String()} {
		if _, err := Resolve(t.Context(), source, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"", "ftp://example.com/plugin", "https://user:secret@example.com/plugin", "https://example.com/p#wat", "file://remote/plugin"} {
		if _, _, _, err := parseSource(source); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %v", source, err)
		}
	}
	for _, source := range []string{"C:/plugins/ruff.wasm", `C:\plugins\ruff.wasm`} {
		_, local, _, err := parseSource(source)
		if err != nil || local != source {
			t.Fatalf("Windows path parsed as URI: %v %s", err, local)
		}
	}
}
