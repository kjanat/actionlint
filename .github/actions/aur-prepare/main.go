// Command aur-prepare rewrites a checked-in AUR PKGBUILD so it describes a
// specific release, and is run by the aur-prepare composite action.
//
// The checked-in PKGBUILDs carry a reference snapshot of pkgver and the
// checksums. This rewrites them in place just before the deploy step pushes to
// the AUR, so nothing has to be bumped by hand:
//
//   - every package gets pkgver set to the release version and pkgrel reset
//     to 1, a fresh upstream version being a new package rather than a new
//     build of the old one;
//   - the prebuilt (-bin) package additionally gets a per-architecture
//     sha256sums_<carch> injected, read from the release's published checksums
//     manifest;
//   - the VCS (-git) package instead gets the pkgver that `git describe` would
//     produce for a checkout at this tag, since its real version is computed by
//     pkgver() at build time and the checked-in value only decides what the AUR
//     web page displays.
//
// Why not updpkgsums for the -bin package? On an x86_64 runner it only fetches
// the sources matching the host $CARCH, silently leaving sha256sums_aarch64 and
// sha256sums_armv7h untouched. Reading the already-published checksums manifest
// covers every architecture from a single small download instead of pulling the
// multi-megabyte tarballs. The source package has one arch-independent source,
// so the workflow lets the deploy action run updpkgsums for it.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// binArches maps an Arch Linux $CARCH to the GOOS_GOARCH pair GoReleaser puts
// in the release asset basename. Kept in lockstep with the source_<arch> arrays
// in aur/actionlint-kjanat-bin/PKGBUILD. armv7h takes the armv6 build, which
// runs on armv7 hardware. A slice rather than a map so the rewrite order, and
// therefore the log output, is deterministic.
var binArches = []struct{ carch, asset string }{
	{"x86_64", "linux_amd64"},
	{"aarch64", "linux_arm64"},
	{"armv7h", "linux_armv6"},
}

// semverRe rejects anything that is not a strict version with an optional
// prerelease. Everything downstream substitutes this into a PKGBUILD, so
// constraining the alphabet to [0-9A-Za-z.-] keeps the result literal.
var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// shaRe matches an abbreviated or full git object name.
var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// sha256Re matches a bare hex digest as it appears in the checksums manifest.
var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

var (
	pkgverRe = regexp.MustCompile(`(?m)^pkgver=.*$`)
	pkgrelRe = regexp.MustCompile(`(?m)^pkgrel=.*$`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "aur-prepare: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		pkg     = os.Getenv("INPUT_PACKAGE")
		version = os.Getenv("INPUT_VERSION")
		commit  = os.Getenv("INPUT_COMMIT")
		repo    = os.Getenv("INPUT_REPOSITORY")
		token   = os.Getenv("INPUT_TOKEN")
		dir     = os.Getenv("INPUT_AUR_DIR")
	)
	if dir == "" {
		// The action passes an absolute path; this fallback is for local runs
		// from a checkout root.
		if ws := os.Getenv("GITHUB_WORKSPACE"); ws != "" {
			dir = filepath.Join(ws, "aur")
		} else {
			dir = "aur"
		}
	}
	if pkg == "" {
		return errors.New("package is required")
	}
	version = strings.TrimPrefix(version, "v")
	if !semverRe.MatchString(version) {
		return fmt.Errorf("version %q is not X.Y.Z or X.Y.Z-prerelease", version)
	}

	path := filepath.Join(dir, pkg, "PKGBUILD")
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	rewritten, err := prepare(string(original), pkg, version, commit, func(name string) (string, error) {
		return fetchChecksum(repo, version, name, token)
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		return err
	}

	fmt.Printf("--- prepared %s ---\n%s", path, rewritten)
	return nil
}

// prepare returns contents with the release's version, and for the prebuilt
// package its checksums, substituted in. checksumFor resolves the sha256 of a
// named release asset and is only consulted for the -bin package, so the other
// two packages need no network access at all.
func prepare(contents, pkg, version, commit string, checksumFor func(asset string) (string, error)) (string, error) {
	var pkgver string
	switch {
	case strings.HasSuffix(pkg, "-git"):
		// Exactly what `git describe --long --tags` yields at this tag.
		if !shaRe.MatchString(commit) {
			return "", fmt.Errorf("the -git package needs a commit sha, got %q", commit)
		}
		pkgver = fmt.Sprintf("%s.r0.g%s", version, commit[:7])
	default:
		pkgver = version
	}

	out, err := replaceLine(contents, pkgverRe, "pkgver="+pkgver)
	if err != nil {
		return "", err
	}
	if out, err = replaceLine(out, pkgrelRe, "pkgrel=1"); err != nil {
		return "", err
	}

	if !strings.HasSuffix(pkg, "-bin") {
		return out, nil
	}

	for _, a := range binArches {
		asset := fmt.Sprintf("actionlint_%s_%s.tar.gz", version, a.asset)
		sum, err := checksumFor(asset)
		if err != nil {
			return "", err
		}
		if !sha256Re.MatchString(sum) {
			return "", fmt.Errorf("checksum for %s is not a sha256 digest: %q", asset, sum)
		}
		re := regexp.MustCompile(`(?m)^sha256sums_` + regexp.QuoteMeta(a.carch) + `=\(.*\)$`)
		if out, err = replaceLine(out, re, fmt.Sprintf("sha256sums_%s=('%s')", a.carch, sum)); err != nil {
			return "", err
		}
	}
	return out, nil
}

// replaceLine substitutes every match of re with repl, treating repl literally
// so a digest or version can never be read as a $1-style expansion. A PKGBUILD
// that no longer contains the anchor is an error rather than a silent no-op:
// pushing an unrewritten PKGBUILD would publish the previous release's version.
func replaceLine(src string, re *regexp.Regexp, repl string) (string, error) {
	if !re.MatchString(src) {
		return "", fmt.Errorf("no line matching %s to replace with %q", re, repl)
	}
	return re.ReplaceAllLiteralString(src, repl), nil
}

// checksumsRe pulls the digest for a named file out of a sha256sum-style
// manifest, whose lines are "<digest>  <filename>".
func checksumsRe(asset string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^([0-9a-f]{64}) [ *]?` + regexp.QuoteMeta(asset) + `$`)
}

// fetchChecksum downloads the release's checksums manifest and returns the
// digest recorded for asset. The manifest is fetched once per asset; it is a
// few kilobytes, and the alternative is threading a cache through prepare for
// no measurable gain on three lookups.
func fetchChecksum(repo, version, asset, token string) (string, error) {
	name := fmt.Sprintf("actionlint_%s_checksums.txt", version)
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repo, version, name)

	body, err := get(url, token)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	m := checksumsRe(asset).FindStringSubmatch(string(body))
	if m == nil {
		return "", fmt.Errorf("%s lists no digest for %s", name, asset)
	}
	return m[1], nil
}

// get retrieves url, retrying briefly so a release asset that has not finished
// propagating to the CDN does not fail the publish. The token is sent only for
// github.com; Go's client drops the Authorization header across a redirect to
// another host, which is what the asset CDN requires.
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
		body, readErr := io.ReadAll(resp.Body)
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
