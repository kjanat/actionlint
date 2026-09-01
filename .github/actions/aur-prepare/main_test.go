package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A trimmed stand-in for the prebuilt PKGBUILD, carrying only the lines the
// rewrite touches.
const binPKGBUILD = `pkgname=actionlint-kjanat-bin
pkgver=1.0.0
pkgrel=7
sha256sums_x86_64=('SKIP')
sha256sums_aarch64=('SKIP')
sha256sums_armv7h=('SKIP')
`

const srcPKGBUILD = `pkgname=actionlint-kjanat
pkgver=1.0.0
pkgrel=7
sha256sums=('SKIP')
`

const gitPKGBUILD = `pkgname=actionlint-kjanat-git
pkgver=1.0.0.r0.g0000000
pkgrel=7
`

// digest returns a distinct, well-formed sha256 for each asset so a
// cross-architecture mix-up is visible in the assertions.
func digest(n int) string { return strings.Repeat(fmt.Sprintf("%x", n), 64) }

func stubChecksums(t *testing.T) func(string) (string, error) {
	t.Helper()
	sums := map[string]string{
		"actionlint_2.3.4_linux_amd64.tar.gz": digest(1),
		"actionlint_2.3.4_linux_arm64.tar.gz": digest(2),
		"actionlint_2.3.4_linux_armv6.tar.gz": digest(3),
	}
	return func(asset string) (string, error) {
		sum, ok := sums[asset]
		if !ok {
			return "", fmt.Errorf("unexpected asset %q", asset)
		}
		return sum, nil
	}
}

// noNetwork fails the test if it is called, proving the source and VCS packages
// are prepared without touching the network.
func noNetwork(t *testing.T) func(string) (string, error) {
	t.Helper()
	return func(asset string) (string, error) {
		t.Errorf("unexpected checksum lookup for %q", asset)
		return "", nil
	}
}

func TestPrepareBinInjectsPerArchChecksums(t *testing.T) {
	out, err := prepare(binPKGBUILD, "actionlint-kjanat-bin", "2.3.4", "", stubChecksums(t))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, want := range []string{
		"pkgver=2.3.4",
		"pkgrel=1",
		fmt.Sprintf("sha256sums_x86_64=('%s')", digest(1)),
		fmt.Sprintf("sha256sums_aarch64=('%s')", digest(2)),
		fmt.Sprintf("sha256sums_armv7h=('%s')", digest(3)),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SKIP") {
		t.Errorf("a SKIP placeholder survived the rewrite:\n%s", out)
	}
}

func TestPrepareSourceNeedsNoChecksumLookup(t *testing.T) {
	out, err := prepare(srcPKGBUILD, "actionlint-kjanat", "2.3.4", "", noNetwork(t))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.Contains(out, "pkgver=2.3.4") || !strings.Contains(out, "pkgrel=1") {
		t.Errorf("version not rewritten:\n%s", out)
	}
	// updpkgsums refreshes this one in the deploy step, so it must be left as is.
	if !strings.Contains(out, "sha256sums=('SKIP')") {
		t.Errorf("source checksums should be left for updpkgsums:\n%s", out)
	}
}

func TestPrepareGitStampsDescribeVersion(t *testing.T) {
	out, err := prepare(gitPKGBUILD, "actionlint-kjanat-git", "2.3.4", "abcdef1234567890", noNetwork(t))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// What `git describe --long --tags` yields for a checkout at this tag.
	if !strings.Contains(out, "pkgver=2.3.4.r0.gabcdef1") {
		t.Errorf("expected a describe-shaped pkgver:\n%s", out)
	}
}

func TestPrepareGitRequiresCommit(t *testing.T) {
	for _, commit := range []string{"", "nope", "abc"} {
		if _, err := prepare(gitPKGBUILD, "actionlint-kjanat-git", "2.3.4", commit, noNetwork(t)); err == nil {
			t.Errorf("commit %q: expected an error", commit)
		}
	}
}

// A PKGBUILD that no longer carries an anchor must fail loudly: pushing it
// unrewritten would republish the previous release's version.
func TestPrepareFailsWhenAnchorMissing(t *testing.T) {
	if _, err := prepare("pkgname=actionlint-kjanat\n", "actionlint-kjanat", "2.3.4", "", noNetwork(t)); err == nil {
		t.Error("expected an error when pkgver= is absent")
	}
	missingArch := strings.ReplaceAll(binPKGBUILD, "sha256sums_armv7h=('SKIP')\n", "")
	if _, err := prepare(missingArch, "actionlint-kjanat-bin", "2.3.4", "", stubChecksums(t)); err == nil {
		t.Error("expected an error when an arch's sha256sums line is absent")
	}
}

func TestPrepareRejectsNonDigest(t *testing.T) {
	_, err := prepare(binPKGBUILD, "actionlint-kjanat-bin", "2.3.4", "", func(string) (string, error) {
		return "not-a-digest", nil
	})
	if err == nil {
		t.Error("expected an error for a malformed digest")
	}
}

// A digest is substituted literally; regexp expansion would corrupt it if a
// replacement were ever read as $1-style syntax.
func TestReplaceLineTreatsReplacementLiterally(t *testing.T) {
	out, err := replaceLine("pkgver=old\n", pkgverRe, "pkgver=$1x")
	if err != nil {
		t.Fatalf("replaceLine: %v", err)
	}
	if !strings.Contains(out, "pkgver=$1x") {
		t.Errorf("replacement was expanded rather than literal: %q", out)
	}
}

func TestChecksumsReMatchesManifestLines(t *testing.T) {
	manifest := digest(4) + "  actionlint_2.3.4_linux_amd64.tar.gz\n" +
		digest(5) + "  actionlint_2.3.4_darwin_arm64.tar.gz\n"
	m := checksumsRe("actionlint_2.3.4_linux_amd64.tar.gz").FindStringSubmatch(manifest)
	if m == nil || m[1] != digest(4) {
		t.Errorf("did not extract the amd64 digest: %v", m)
	}
	// A name that is a suffix of another entry must not match it.
	if checksumsRe("linux_amd64.tar.gz").MatchString(manifest) {
		t.Error("a partial filename should not match a manifest line")
	}
}

func TestSemverReAcceptsPrereleaseOnly(t *testing.T) {
	for _, ok := range []string{"1.2.3", "1.2.3-rc.1", "10.0.0-beta1"} {
		if !semverRe.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"1.2", "v1.2.3", "1.2.3/x", "1.2.3\nrm -rf", "1.2.3 &"} {
		if semverRe.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// Guards the assumption that every arch the -bin PKGBUILD declares is covered.
func TestBinArchesCoverPKGBUILD(t *testing.T) {
	for _, a := range binArches {
		re := regexp.MustCompile(`(?m)^sha256sums_` + regexp.QuoteMeta(a.carch) + `=\(`)
		if !re.MatchString(binPKGBUILD) {
			t.Errorf("no sha256sums_%s line for a declared arch", a.carch)
		}
	}
}

// The rewrite is anchored to lines in the checked-in PKGBUILDs, so exercise the
// real files as well as the fixtures above. Catches a renamed or dropped anchor.
func TestPrepareAgainstCheckedInPKGBUILDs(t *testing.T) {
	const root = "../../../distribution" // this package sits at .github/actions/aur-prepare

	for _, tc := range []struct {
		pkg    string
		commit string
		want   string
	}{
		{"actionlint-kjanat", "", "pkgver=2.3.4"},
		{"actionlint-kjanat-bin", "", "pkgver=2.3.4"},
		{"actionlint-kjanat-git", "abcdef1234567890", "pkgver=2.3.4.r0.gabcdef1"},
	} {
		t.Run(tc.pkg, func(t *testing.T) {
			path := filepath.Join(root, "aur", tc.pkg, "PKGBUILD")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			out, err := prepare(string(contents), tc.pkg, "2.3.4", tc.commit, stubChecksums(t))
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected %q in the rewritten PKGBUILD", tc.want)
			}
			if !strings.Contains(out, "pkgrel=1") {
				t.Error("pkgrel was not reset to 1")
			}
			// Nothing may be left for a human to bump by hand.
			if strings.HasSuffix(tc.pkg, "-bin") && strings.Contains(out, "SKIP") {
				t.Error("a SKIP placeholder survived the rewrite")
			}
		})
	}
}
