package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarGz builds a GoReleaser-shaped archive: the executable at the root
// alongside the documentation files.
func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(n string, b []byte, mode int64) {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", []byte("readme"), 0o644)
	write(name, body, 0o755)
	write("man/actionlint.1", []byte("man"), 0o644)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		n string
		b []byte
	}{{"README.md", []byte("readme")}, {name, body}} {
		w, err := zw.Create(f.n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixture lays out a workspace with the real distribution/npm sources and a
// downloads directory of synthetic release archives plus a checksums manifest.
func fixture(t *testing.T, version string) *config {
	t.Helper()
	const repoRoot = "../../.." // this package sits at .github/actions/npm-packages

	work := t.TempDir()
	npmDir := filepath.Join(work, "distribution", "npm")
	if err := copyTree(filepath.Join(repoRoot, "distribution", "npm"), npmDir); err != nil {
		t.Fatalf("staging distribution/npm: %v", err)
	}
	if err := copyFile(filepath.Join(repoRoot, "LICENSE.txt"), filepath.Join(work, "LICENSE.txt")); err != nil {
		t.Fatalf("staging LICENSE.txt: %v", err)
	}

	tf, err := loadTargets(filepath.Join(npmDir, "targets.json"))
	if err != nil {
		t.Fatalf("loadTargets: %v", err)
	}

	downloads := filepath.Join(work, "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config{
		version:   version,
		repoRoot:  work,
		npmDir:    npmDir,
		outDir:    filepath.Join(work, "dist"),
		downloads: downloads,
	}

	var manifest strings.Builder
	for _, target := range tf.Targets {
		exe := tf.Binary
		if target.OS == "win32" {
			exe += ".exe"
		}
		// Distinct contents per target so a cross-wired package is detectable.
		body := []byte("binary for " + target.Pkg)
		var archive []byte
		if target.Format == "zip" {
			archive = zipArchive(t, exe, body)
		} else {
			archive = tarGz(t, exe, body)
		}
		name := cfg.assetName(target)
		if err := os.WriteFile(filepath.Join(downloads, name), archive, 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(sha256Sum(archive)), name)
	}
	sums := fmt.Sprintf("actionlint_%s_checksums.txt", version)
	if err := os.WriteFile(filepath.Join(downloads, sums), []byte(manifest.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return m
}

func TestBuildsEveryTargetAndTheFacade(t *testing.T) {
	const version = "3.2.1"
	cfg := fixture(t, version)

	tf, err := loadTargets(filepath.Join(cfg.npmDir, "targets.json"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := cfg.checksums()
	if err != nil {
		t.Fatalf("checksums: %v", err)
	}
	for _, target := range tf.Targets {
		if err := cfg.buildPlatformPackage(tf, target, sums); err != nil {
			t.Fatalf("%s: %v", target.Pkg, err)
		}
	}
	if err := cfg.buildFacade(tf); err != nil {
		t.Fatalf("facade: %v", err)
	}

	for _, target := range tf.Targets {
		dir := filepath.Join(cfg.outDir, target.Pkg)
		exe := tf.Binary
		if target.OS == "win32" {
			exe += ".exe"
		}

		bin := filepath.Join(dir, "bin", exe)
		got, err := os.ReadFile(bin)
		if err != nil {
			t.Fatalf("%s: %v", target.Pkg, err)
		}
		// Proves each package got its own binary, not a neighbour's.
		if want := "binary for " + target.Pkg; string(got) != want {
			t.Errorf("%s: binary is %q, want %q", target.Pkg, got, want)
		}
		info, err := os.Stat(bin)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("%s: binary is not executable (%v)", target.Pkg, info.Mode())
		}

		manifest := readJSON(t, filepath.Join(dir, "package.json"))
		if name := manifest["name"]; name != tf.Facade+"-"+target.Pkg {
			t.Errorf("%s: name is %v", target.Pkg, name)
		}
		if manifest["version"] != version {
			t.Errorf("%s: version is %v", target.Pkg, manifest["version"])
		}
		// os and cpu are what stop npm installing this package elsewhere.
		if osv, ok := manifest["os"].([]any); !ok || len(osv) != 1 || osv[0] != target.OS {
			t.Errorf("%s: os is %v", target.Pkg, manifest["os"])
		}
		if cpu, ok := manifest["cpu"].([]any); !ok || len(cpu) != 1 || cpu[0] != target.CPU {
			t.Errorf("%s: cpu is %v", target.Pkg, manifest["cpu"])
		}
		for _, f := range []string{"README.md", "LICENSE.txt"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Errorf("%s: missing %s", target.Pkg, f)
			}
		}
	}

	facadeDir := filepath.Join(cfg.outDir, "facade")
	facade := readJSON(t, filepath.Join(facadeDir, "package.json"))
	if facade["version"] != version {
		t.Errorf("facade version is %v", facade["version"])
	}
	optional, ok := facade["optionalDependencies"].(map[string]any)
	if !ok {
		t.Fatalf("facade optionalDependencies is %T", facade["optionalDependencies"])
	}
	if len(optional) != len(tf.Targets) {
		t.Errorf("facade declares %d platform packages, want %d", len(optional), len(tf.Targets))
	}
	for _, target := range tf.Targets {
		name := tf.Facade + "-" + target.Pkg
		// Exact pins: a range could pair this facade with another release's binary.
		if optional[name] != version {
			t.Errorf("facade pins %s to %v, want the exact %s", name, optional[name], version)
		}
	}
	// The launcher sources have to travel with the facade or its bin shim breaks.
	for _, f := range []string{"bin/actionlint.mjs", "lib/resolve.mjs", "lib/launch.mjs", "README.md", "LICENSE.txt"} {
		if _, err := os.Stat(filepath.Join(facadeDir, filepath.FromSlash(f))); err != nil {
			t.Errorf("facade is missing %s", f)
		}
	}
	// The bin shim is spawned by npm, so it must stay executable.
	if info, err := os.Stat(filepath.Join(facadeDir, "bin", "actionlint.mjs")); err == nil {
		if info.Mode()&0o111 == 0 {
			t.Errorf("facade bin shim is not executable (%v)", info.Mode())
		}
	}
}

// A corrupted or swapped asset must stop the build, not reach npm.
func TestRejectsChecksumMismatch(t *testing.T) {
	cfg := fixture(t, "3.2.1")
	tf, err := loadTargets(filepath.Join(cfg.npmDir, "targets.json"))
	if err != nil {
		t.Fatal(err)
	}
	target := tf.Targets[0]
	sums, err := cfg.checksums()
	if err != nil {
		t.Fatal(err)
	}
	sums[cfg.assetName(target)] = strings.Repeat("0", 64)

	err = cfg.buildPlatformPackage(tf, target, sums)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected a digest mismatch, got %v", err)
	}
}

func TestRejectsAssetWithNoRecordedDigest(t *testing.T) {
	cfg := fixture(t, "3.2.1")
	tf, err := loadTargets(filepath.Join(cfg.npmDir, "targets.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.buildPlatformPackage(tf, tf.Targets[0], map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "no digest") {
		t.Fatalf("expected a missing-digest error, got %v", err)
	}
}

// The resolver derives <facade>-<os>-<cpu>, so a target named anything else
// would publish a package it could never find.
func TestTargetNamesMatchOsAndCpu(t *testing.T) {
	tf, err := loadTargets(filepath.Join("../../..", "distribution", "npm", "targets.json"))
	if err != nil {
		t.Fatalf("the checked-in targets.json is invalid: %v", err)
	}
	if len(tf.Targets) == 0 {
		t.Fatal("targets.json declares no targets")
	}
	seen := map[string]bool{}
	for _, target := range tf.Targets {
		if seen[target.Pkg] {
			t.Errorf("duplicate target %q", target.Pkg)
		}
		seen[target.Pkg] = true
	}
}

func TestLoadTargetsRejectsMismatchedName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "targets.json")
	bad := `{"facade":"@kjanat/actionlint","binary":"actionlint","targets":[
		{"pkg":"linux-amd64","os":"linux","cpu":"x64","asset":"linux_amd64","format":"tar.gz"}]}`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTargets(path); err == nil {
		t.Error("expected a name mismatch to be rejected")
	}
}

func TestExtractBinaryFindsRootMember(t *testing.T) {
	body := []byte("payload")
	got, err := extractBinary(tarGz(t, "actionlint", body), "tar.gz", "actionlint")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("tar.gz: %v %q", err, got)
	}
	got, err = extractBinary(zipArchive(t, "actionlint.exe", body), "zip", "actionlint.exe")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("zip: %v %q", err, got)
	}
	if _, err := extractBinary(tarGz(t, "actionlint", body), "tar.gz", "absent"); err == nil {
		t.Error("expected an error for a missing member")
	}
}

// Key order in a published manifest is read by people; a map round-trip would
// scramble it.
func TestObjectPreservesKeyOrder(t *testing.T) {
	src := []byte(`{"name":"x","version":"1","license":"MIT","nested":{"b":1,"a":2}}`)
	o, err := parseObject(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.set("version", "2"); err != nil {
		t.Fatal(err)
	}
	if err := o.set("appended", true); err != nil {
		t.Fatal(err)
	}
	out, err := o.encode()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"name", "version", "license", "nested", "appended"}
	var order []string
	for _, key := range want {
		idx := bytes.Index(out, []byte(`"`+key+`"`))
		if idx < 0 {
			t.Fatalf("key %q missing from:\n%s", key, out)
		}
		order = append(order, key)
	}
	for i := 1; i < len(order); i++ {
		if bytes.Index(out, []byte(`"`+order[i-1]+`"`)) > bytes.Index(out, []byte(`"`+order[i]+`"`)) {
			t.Errorf("key %q sorted before %q:\n%s", order[i], order[i-1], out)
		}
	}
	if !bytes.Contains(out, []byte(`"version": "2"`)) {
		t.Errorf("set did not replace in place:\n%s", out)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
}
