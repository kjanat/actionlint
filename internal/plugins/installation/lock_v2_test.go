package installation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

var lockedSource = "oci://ghcr.io/kjanat/provider@sha256:" + strings.Repeat("a", 64)

func lockedPlatforms() map[string]artifacts.OCIPlatform {
	return map[string]artifacts.OCIPlatform{
		"linux/amd64":   {Manifest: strings.Repeat("b", 64), Blobs: []string{strings.Repeat("c", 64), strings.Repeat("d", 64)}},
		"windows/amd64": {Manifest: strings.Repeat("e", 64), Blobs: []string{strings.Repeat("c", 64), strings.Repeat("f", 64)}},
	}
}

func sourcePin(t *testing.T) plugininstall.SourcePin {
	t.Helper()
	pin, err := plugininstall.NewSourcePin(lockedSource, lockedPlatforms())
	if err != nil {
		t.Fatal(err)
	}
	return pin
}

// A schema 1 lock is read and rewritten as schema 2, which adds the
// per-platform blobs of each oci:// source.
func TestLockV1RewrittenAsV2(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), plugininstall.LockName)
	if err := os.WriteFile(path, []byte("{\"schema_version\":1,\"plugins\":[]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := plugininstall.OpenLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Lock.SchemaVersion != 1 {
		t.Fatalf("schema = %d", f.Lock.SchemaVersion)
	}
	f.Lock.Sources = []plugininstall.SourcePin{sourcePin(t)}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		SchemaVersion int `json:"schema_version"`
		Sources       []struct {
			Source    string                           `json:"source"`
			Platforms map[string]artifacts.OCIPlatform `json:"platforms"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	if written.SchemaVersion != 2 || len(written.Sources) != 1 || written.Sources[0].Source != lockedSource || len(written.Sources[0].Platforms) != 2 {
		t.Fatalf("written lock = %s", data)
	}
	reopened, err := plugininstall.OpenLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	pin, ok := reopened.Lock.SourcePin(lockedSource)
	if reopened.Lock.SchemaVersion != plugininstall.LockSchemaVersion || !ok {
		t.Fatalf("reopened = %+v", reopened.Lock)
	}
	if got, _ := pin.Digests("windows/amd64"); !slices.Equal(got, []string{strings.Repeat("a", 64), strings.Repeat("e", 64), strings.Repeat("c", 64), strings.Repeat("f", 64)}) {
		t.Fatalf("round-tripped digests = %q", got)
	}
}

func TestSourcePinRejected(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	for name, edit := range map[string]func(source *string, platforms map[string]artifacts.OCIPlatform){
		"tag reference": func(source *string, _ map[string]artifacts.OCIPlatform) { *source = "oci://ghcr.io/kjanat/provider:v1" },
		"https source": func(source *string, _ map[string]artifacts.OCIPlatform) {
			*source = "https://example.com/p.wasm#sha256=" + digest
		},
		"unnormalized host": func(source *string, _ map[string]artifacts.OCIPlatform) {
			*source = "oci://GHCR.io/kjanat/provider@sha256:" + digest
		},
		"no platforms": func(_ *string, platforms map[string]artifacts.OCIPlatform) { clear(platforms) },
		"invalid platform": func(_ *string, platforms map[string]artifacts.OCIPlatform) {
			platforms["linux"] = platforms["linux/amd64"]
		},
		"invalid blob digest": func(_ *string, platforms map[string]artifacts.OCIPlatform) {
			platforms["linux/amd64"] = artifacts.OCIPlatform{Manifest: digest, Blobs: []string{"sha256:" + digest}}
		},
		"platform without blobs": func(_ *string, platforms map[string]artifacts.OCIPlatform) {
			platforms["linux/amd64"] = artifacts.OCIPlatform{Manifest: digest}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, platforms := lockedSource, lockedPlatforms()
			edit(&source, platforms)
			if _, err := plugininstall.NewSourcePin(source, platforms); err == nil {
				t.Fatal("NewSourcePin accepted")
			}
			document, err := json.Marshal(map[string]any{"schema_version": 2, "plugins": []any{}, "sources": []any{map[string]any{"source": source, "platforms": platforms}}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), plugininstall.LockName)
			if err := os.WriteFile(path, document, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := plugininstall.OpenLock(t.Context(), path); err == nil {
				t.Fatal("OpenLock accepted")
			}
		})
	}
}

func TestLockSourcesRejected(t *testing.T) {
	t.Parallel()
	for name, lock := range map[string]plugininstall.Lock{
		"duplicate source":    {SchemaVersion: 2, Plugins: []plugininstall.Pin{}, Sources: []plugininstall.SourcePin{sourcePin(t), sourcePin(t)}},
		"sources in schema 1": {SchemaVersion: 1, Plugins: []plugininstall.Pin{}, Sources: []plugininstall.SourcePin{sourcePin(t)}},
		"unknown schema":      {SchemaVersion: 3, Plugins: []plugininstall.Pin{}},
		"zero source pin":     {SchemaVersion: 2, Plugins: []plugininstall.Pin{}, Sources: []plugininstall.SourcePin{{}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := lock.Validate(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestSourcePinDigestsAndVerify(t *testing.T) {
	t.Parallel()
	pin := sourcePin(t)
	got, ok := pin.Digests("linux/amd64")
	if !ok || !slices.Equal(got, []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)}) {
		t.Fatalf("Digests = %q, %t", got, ok)
	}
	if _, ok := pin.Digests("darwin/arm64"); ok {
		t.Fatal("unserved platform listed")
	}
	lone, err := plugininstall.NewSourcePin(lockedSource, map[string]artifacts.OCIPlatform{artifacts.AnyPlatform: lockedPlatforms()["linux/amd64"]})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lone.Digests("darwin/arm64"); !ok {
		t.Fatal("a lone image manifest serves every platform")
	}
	info := artifacts.Info{Platform: artifacts.AnyPlatform, Manifest: strings.Repeat("b", 64), Blobs: []string{strings.Repeat("c", 64), strings.Repeat("d", 64)}}
	if err := lone.Verify(info); err != nil {
		t.Fatal(err)
	}
	info.Blobs = info.Blobs[:1]
	if err := lone.Verify(info); err == nil {
		t.Fatal("a pull with other layers verified")
	}
}
