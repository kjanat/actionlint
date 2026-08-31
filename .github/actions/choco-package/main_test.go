package main

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsZip(t *testing.T, exe, license []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		n string
		b []byte
	}{{"actionlint.exe", exe}, {"LICENSE.txt", license}, {"README.md", []byte("readme")}} {
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

func fixture(t *testing.T, version string) (*config, []byte) {
	t.Helper()
	work := t.TempDir()
	downloads := filepath.Join(work, "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := []byte("MZ fake actionlint binary")
	archive := windowsZip(t, exe, []byte("the MIT License"))
	asset := fmt.Sprintf("actionlint_%s_windows_amd64.zip", version)
	if err := os.WriteFile(filepath.Join(downloads, asset), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sha256Sum(archive)), asset)
	sums := fmt.Sprintf("actionlint_%s_checksums.txt", version)
	if err := os.WriteFile(filepath.Join(downloads, sums), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return &config{
		version:    version,
		repository: "kjanat/actionlint",
		outDir:     filepath.Join(work, "dist"),
		downloads:  downloads,
	}, exe
}

// Drives run() the way the action does, through the environment.
func runWith(t *testing.T, cfg *config) error {
	t.Helper()
	t.Setenv("INPUT_VERSION", cfg.version)
	t.Setenv("INPUT_REPOSITORY", cfg.repository)
	t.Setenv("INPUT_OUT_DIR", cfg.outDir)
	t.Setenv("INPUT_DOWNLOADS", cfg.downloads)
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "out.txt"))
	return run()
}

func TestLaysOutPackage(t *testing.T) {
	cfg, exe := fixture(t, "1.13.0")
	if err := runWith(t, cfg); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(cfg.outDir, "tools", "actionlint.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, exe) {
		t.Error("the embedded binary is not the one from the archive")
	}
	// Chocolatey's moderators require both of these beside an embedded binary.
	for _, f := range []string{"LICENSE.txt", "VERIFICATION.txt"} {
		if _, err := os.Stat(filepath.Join(cfg.outDir, "tools", f)); err != nil {
			t.Errorf("missing tools/%s", f)
		}
	}

	raw, err := os.ReadFile(filepath.Join(cfg.outDir, packageID+".nuspec"))
	if err != nil {
		t.Fatal(err)
	}
	var doc nuspec
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("nuspec is not valid XML: %v", err)
	}
	if doc.Metadata.ID != "actionlint-kjanat" {
		t.Errorf("id is %q; the plain actionlint id belongs to upstream", doc.Metadata.ID)
	}
	if doc.Metadata.Version != "1.13.0" {
		t.Errorf("version is %q", doc.Metadata.Version)
	}
	if doc.Metadata.RequireLicenseAcceptance {
		t.Error("requireLicenseAcceptance should be false for an MIT tool")
	}
	for _, want := range []string{"id", "version", "authors", "description"} {
		if !bytes.Contains(raw, []byte("<"+want+">")) {
			t.Errorf("nuspec is missing the required <%s> element", want)
		}
	}
	// The package page should not imply this is the upstream project.
	if !strings.Contains(doc.Metadata.Description, "not the upstream project") {
		t.Error("description should say this is the fork")
	}
}

// The VERIFICATION file is only useful if the checksum in it is the real one.
func TestVerificationCarriesTheRealChecksum(t *testing.T) {
	cfg, _ := fixture(t, "1.13.0")
	if err := runWith(t, cfg); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(cfg.outDir, "tools", "VERIFICATION.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := cfg.checksum("actionlint_1.13.0_windows_amd64.zip")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text, []byte(want)) {
		t.Errorf("VERIFICATION.txt does not carry the published digest %s", want)
	}
}

func TestChecksumMismatchStopsTheBuild(t *testing.T) {
	cfg, _ := fixture(t, "1.13.0")
	// Corrupt the archive so it no longer matches the published manifest.
	asset := filepath.Join(cfg.downloads, "actionlint_1.13.0_windows_amd64.zip")
	data, err := os.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, append(data, 'x'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runWith(t, cfg)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected a digest mismatch, got %v", err)
	}
}

// A release candidate must not become the default install on the community feed.
func TestPrereleaseIsSkipped(t *testing.T) {
	cfg, _ := fixture(t, "1.13.0")
	cfg.version = "1.13.0-rc.1"
	if err := runWith(t, cfg); err != nil {
		t.Fatalf("a prerelease should skip cleanly, got %v", err)
	}
	if _, err := os.Stat(cfg.outDir); !os.IsNotExist(err) {
		t.Error("a prerelease should not lay out a package")
	}
}

func TestIsPrerelease(t *testing.T) {
	for _, v := range []string{"1.13.0-rc.1", "1.0.0-beta1", "2.3.4-0"} {
		if !isPrerelease(v) {
			t.Errorf("%q should be a prerelease", v)
		}
	}
	for _, v := range []string{"1.13.0", "10.0.1"} {
		if isPrerelease(v) {
			t.Errorf("%q should not be a prerelease", v)
		}
	}
}

func TestRejectsBadVersion(t *testing.T) {
	cfg, _ := fixture(t, "1.13.0")
	cfg.version = "1.13"
	if err := runWith(t, cfg); err == nil {
		t.Error("expected a malformed version to be rejected")
	}
}
