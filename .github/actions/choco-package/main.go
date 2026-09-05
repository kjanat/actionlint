// Command choco-package lays out a Chocolatey package for actionlint, and is
// run by the choco-package composite action.
//
// GoReleaser's chocolateys pipe is unusable here: it declares choco as a
// required dependency and shells out to it, making it Windows-only, while the
// release runs on Linux. A chocolateys block in .goreleaser.yaml fails the
// release's preflight check. This runs as a separate Windows job reading the
// already-published release.
//
// The executable is embedded in the package. Chocolatey shims any .exe under
// tools/ automatically, needing no install script, and an embedded binary
// survives a release asset moving. Chocolatey's moderators require LICENSE.txt
// and VERIFICATION.txt alongside an embedded binary, and both are written too.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxArchive = 256 << 20 // 256 MiB

// The Chocolatey community feed already carries "actionlint", which packages
// upstream. This fork needs its own id.
const packageID = "actionlint-kjanat"

var semverRe = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.-]+)?$`)

// nuspec mirrors the subset of the nuspec schema this package needs. Field
// order is the marshalling order, and matches Chocolatey's own template.
type nuspec struct {
	XMLName  xml.Name `xml:"package"`
	Xmlns    string   `xml:"xmlns,attr"`
	Metadata metadata `xml:"metadata"`
	Files    []file   `xml:"files>file"`
}

type metadata struct {
	ID                       string `xml:"id"`
	Version                  string `xml:"version"`
	PackageSourceURL         string `xml:"packageSourceUrl"`
	Owners                   string `xml:"owners"`
	Title                    string `xml:"title"`
	Authors                  string `xml:"authors"`
	ProjectURL               string `xml:"projectUrl"`
	Copyright                string `xml:"copyright"`
	LicenseURL               string `xml:"licenseUrl"`
	RequireLicenseAcceptance bool   `xml:"requireLicenseAcceptance"`
	ProjectSourceURL         string `xml:"projectSourceUrl"`
	DocsURL                  string `xml:"docsUrl"`
	BugTrackerURL            string `xml:"bugTrackerUrl"`
	Tags                     string `xml:"tags"`
	Summary                  string `xml:"summary"`
	Description              string `xml:"description"`
	ReleaseNotes             string `xml:"releaseNotes"`
}

type file struct {
	Src    string `xml:"src,attr"`
	Target string `xml:"target,attr"`
}

type config struct {
	version    string
	repository string
	token      string
	outDir     string
	downloads  string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "choco-package: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Prereleases stay off the community feed: NuGet's version grammar
	// disagrees with semver on dotted prerelease identifiers.
	if isPrerelease(cfg.version) {
		fmt.Printf("::notice::skipping Chocolatey for prerelease %s\n", cfg.version)
		return writeOutput("skipped", "true")
	}

	asset := fmt.Sprintf("actionlint_%s_windows_amd64.zip", cfg.version)
	archive, err := cfg.readAsset(asset)
	if err != nil {
		return err
	}
	sum, err := cfg.checksum(asset)
	if err != nil {
		return err
	}
	got := hex.EncodeToString(sha256Sum(archive))
	if got != sum {
		return fmt.Errorf("%s digest mismatch: manifest says %s, downloaded %s", asset, sum, got)
	}

	exe, err := extractZip(archive, "actionlint.exe")
	if err != nil {
		return err
	}
	license, err := extractZip(archive, "LICENSE.txt")
	if err != nil {
		return err
	}

	tools := filepath.Join(cfg.outDir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tools, "actionlint.exe"), exe, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tools, "LICENSE.txt"), license, 0o644); err != nil {
		return err
	}
	verification := verificationText(cfg.repository, cfg.version, asset, sum)
	if err := os.WriteFile(filepath.Join(tools, "VERIFICATION.txt"), []byte(verification), 0o644); err != nil {
		return err
	}

	doc, err := renderNuspec(cfg.repository, cfg.version)
	if err != nil {
		return err
	}
	nuspecPath := filepath.Join(cfg.outDir, packageID+".nuspec")
	if err := os.WriteFile(nuspecPath, doc, 0o644); err != nil {
		return err
	}

	fmt.Printf("laid out %s %s in %s (%d byte exe)\n", packageID, cfg.version, cfg.outDir, len(exe))
	if err := writeOutput("skipped", "false"); err != nil {
		return err
	}
	return writeOutput("nuspec", nuspecPath)
}

func loadConfig() (*config, error) {
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = "."
	}
	cfg := &config{
		version:    strings.TrimPrefix(os.Getenv("INPUT_VERSION"), "v"),
		repository: os.Getenv("INPUT_REPOSITORY"),
		token:      os.Getenv("INPUT_TOKEN"),
		outDir:     os.Getenv("INPUT_OUT_DIR"),
		downloads:  os.Getenv("INPUT_DOWNLOADS"),
	}
	if !semverRe.MatchString(cfg.version) {
		return nil, fmt.Errorf("version %q is not X.Y.Z or X.Y.Z-prerelease", cfg.version)
	}
	if cfg.outDir == "" {
		cfg.outDir = filepath.Join(workspace, "distribution", "chocolatey", "dist")
	}
	return cfg, nil
}

func isPrerelease(version string) bool {
	m := semverRe.FindStringSubmatch(version)
	return m != nil && m[2] != ""
}

// renderNuspec builds the package manifest. Chocolatey renders description as
// Markdown on the package page.
func renderNuspec(repo, version string) ([]byte, error) {
	repoURL := "https://github.com/" + repo
	doc := nuspec{
		Xmlns: "http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd",
		Metadata: metadata{
			ID:                       packageID,
			Version:                  version,
			PackageSourceURL:         repoURL + "/tree/master/.github/actions/choco-package",
			Owners:                   "kjanat",
			Title:                    "actionlint (kjanat fork)",
			Authors:                  "Kaj Kowalski, rhysd",
			ProjectURL:               "https://actionlint.kjanat.dev",
			Copyright:                "Copyright (c) 2026 Kaj Kowalski, Copyright (c) 2021 rhysd",
			LicenseURL:               repoURL + "/blob/master/LICENSE.txt",
			RequireLicenseAcceptance: false,
			ProjectSourceURL:         repoURL,
			DocsURL:                  repoURL + "/blob/master/docs/usage.md",
			BugTrackerURL:            repoURL + "/issues",
			Tags:                     "actionlint github-actions workflow linter static-analysis yaml ci",
			Summary:                  "Static checker for GitHub Actions workflow files",
			Description: "actionlint is a static checker for GitHub Actions workflow files. It checks the workflow syntax, " +
				"the expressions in `${{ }}` placeholders, the shell scripts in `run:` steps, and many other mistakes " +
				"that only surface once a workflow runs.\n\n" +
				"This package installs the [kjanat fork](" + repoURL + "), which is not the upstream project.",
			ReleaseNotes: fmt.Sprintf("%s/releases/tag/v%s", repoURL, version),
		},
		Files: []file{{Src: `tools\**`, Target: "tools"}},
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// verificationText is what Chocolatey's moderators read to confirm the embedded
// binary really came from the upstream release.
func verificationText(repo, version, asset, sum string) string {
	return fmt.Sprintf(`VERIFICATION

Verification is intended to assist the Chocolatey moderators and community in
verifying that this package's contents are trustworthy.

The bundled actionlint.exe is taken verbatim from the GitHub release, which is
built and published by GitHub Actions from the tagged source:

  https://github.com/%[1]s/releases/download/v%[2]s/%[3]s

To verify the binary, download that archive and compare its checksum:

  sha256: %[4]s

  PowerShell:  (Get-FileHash %[3]s -Algorithm SHA256).Hash
  Linux/macOS: sha256sum %[3]s

The checksum above is the one published by the release itself, in
actionlint_%[2]s_checksums.txt, and this package is rejected at build time if
the downloaded archive does not match it.

LICENSE.txt in this directory is the licence file shipped inside that same
archive.
`, repo, version, asset, sum)
}

// checksum returns the release's published digest for a named asset.
func (c *config) checksum(asset string) (string, error) {
	name := fmt.Sprintf("actionlint_%s_checksums.txt", c.version)
	data, err := c.readAsset(name)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s lists no digest for %s", name, asset)
}

func (c *config) readAsset(name string) ([]byte, error) {
	if c.downloads != "" {
		data, err := os.ReadFile(filepath.Join(c.downloads, name))
		if err == nil {
			return data, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if c.repository == "" {
		return nil, fmt.Errorf("%s is not in %s and no repository was given to download it from", name, c.downloads)
	}
	return get(fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", c.repository, c.version, name), c.token)
}

func extractZip(archive []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxArchive))
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("%s is empty", name)
		}
		return data, nil
	}
	return nil, fmt.Errorf("no %s in the archive", name)
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// writeOutput appends a step output for the composite action to branch on.
func writeOutput(key, value string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s=%s\n", key, value)
	return err
}

func get(url, token string) ([]byte, error) {
	var lastErr error
	for attempt := range 4 {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxArchive))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		return body, nil
	}
	return nil, lastErr
}
