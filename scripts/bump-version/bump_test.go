package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureTargets() []*target {
	return []*target{
		{
			path: "docs.md",
			rules: []rule{
				mustRule("package reference", "`tool@(\\d+\\.\\d+\\.\\d+)`", 1),
				mustRule("action reference", `uses: example/tool@v(\d+\.\d+\.\d+)`, 1),
			},
			unrelated: []string{"sarif v2.1.0"},
		},
		{
			path: "download.bash",
			rules: []rule{
				mustRule("default version", `(?m)^version="(\d+\.\d+\.\d+)"$`, 1),
			},
		},
		{
			path: "page.html",
			rules: []rule{
				mustRule("version badge", `id="version">v(\d+\.\d+\.\d+)`, 1),
			},
		},
	}
}

func targetNamed(t *testing.T, ts []*target, path string) *target {
	t.Helper()
	for _, x := range ts {
		if x.path == path {
			return x
		}
	}
	t.Fatalf("no fixture target %q", path)
	return nil
}

func readFixture(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(filepath.Join("testdata", "repo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b := readFixture(t, "repo", e.Name())
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0666); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func mustParse(t *testing.T, s string) version {
	t.Helper()
	v, err := parseVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
		want  version
	}{
		{"1.2.3", true, version{1, 2, 3}},
		{"0.0.0", true, version{0, 0, 0}},
		{"1.11.0", true, version{1, 11, 0}},
		{"v1.2.3", false, version{}},
		{"01.2.3", false, version{}},
		{"1.02.3", false, version{}},
		{"1.2.03", false, version{}},
		{"1.2", false, version{}},
		{"1.2.3.4", false, version{}},
		{"1.2.3-rc1", false, version{}},
		{"", false, version{}},
	}
	for _, tc := range tests {
		got, err := parseVersion(tc.input)
		if tc.ok {
			if err != nil {
				t.Errorf("parseVersion(%q) returned %v", tc.input, err)
				continue
			}
			if got != tc.want {
				t.Errorf("parseVersion(%q) = %v, want %v", tc.input, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("parseVersion(%q) = %v, want an error", tc.input, got)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"1.9.0", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
	}
	for _, tc := range tests {
		got := mustParse(t, tc.left).compare(mustParse(t, tc.right))
		if got != tc.want {
			t.Errorf("%s.compare(%s) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestBumpUpdatesEveryReference(t *testing.T) {
	root := copyFixture(t)
	var out bytes.Buffer
	if err := Bump(root, fixtureTargets(), mustParse(t, "2.0.0"), &out); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"docs.md", "download.bash", "page.html"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		want := readFixture(t, "want", name)
		if !bytes.Equal(got, want) {
			t.Errorf("%s after the bump is:\n%s\nwant:\n%s", name, got, want)
		}
	}

	for _, want := range []string{"docs.md: 2 reference(s) set to 2.0.0", "download.bash: 1 reference(s) set to 2.0.0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not report %q:\n%s", want, out.String())
		}
	}
}

func TestBumpToSameVersionIsIdempotent(t *testing.T) {
	root := copyFixture(t)
	ts := fixtureTargets()
	v := mustParse(t, "2.0.0")
	if err := Bump(root, ts, v, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Bump(root, ts, v, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "docs.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := readFixture(t, "want", "docs.md"); !bytes.Equal(got, want) {
		t.Errorf("docs.md is:\n%s\nwant:\n%s", got, want)
	}
}

func TestScanRejectsMissingOccurrence(t *testing.T) {
	ts := fixtureTargets()
	tgt := targetNamed(t, ts, "docs.md")
	content := bytes.Replace(readFixture(t, "repo", "docs.md"), []byte("- uses: example/tool@v1.2.0\n"), nil, 1)

	_, err := tgt.scan(content)
	if err == nil {
		t.Fatal("scan accepted a file which lost a declared reference")
	}
	if want := "expected 1 occurrence(s) of action reference but found 0"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestScanRejectsExtraOccurrence(t *testing.T) {
	ts := fixtureTargets()
	tgt := targetNamed(t, ts, "docs.md")
	content := append(readFixture(t, "repo", "docs.md"), "\n- uses: example/tool@v1.2.0\n"...)

	_, err := tgt.scan(content)
	if err == nil {
		t.Fatal("scan accepted a file with an extra undeclared reference")
	}
	if want := "expected 1 occurrence(s) of action reference but found 2"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestScanRejectsUndeclaredReference(t *testing.T) {
	ts := fixtureTargets()
	tgt := targetNamed(t, ts, "docs.md")
	content := append(readFixture(t, "repo", "docs.md"), "\nThe container image is example/tool:1.2.0.\n"...)

	_, err := tgt.scan(content)
	if err == nil {
		t.Fatal("scan accepted a version reference which no rule covers")
	}
	for _, want := range []string{"undeclared version reference", "1.2.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestScanRejectsStaleUnrelatedDeclaration(t *testing.T) {
	ts := fixtureTargets()
	tgt := targetNamed(t, ts, "docs.md")
	content := bytes.Replace(readFixture(t, "repo", "docs.md"), []byte("sarif v2.1.0"), []byte("sarif v2.2.0"), 1)

	_, err := tgt.scan(content)
	if err == nil {
		t.Fatal("scan accepted a stale unrelated declaration")
	}
	if want := `unrelated version reference "sarif v2.1.0" no longer appears`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestScanRejectsOverlappingRules(t *testing.T) {
	tgt := &target{
		path: "docs.md",
		rules: []rule{
			mustRule("outer", "`tool@(\\d+\\.\\d+\\.\\d+)`", 1),
			mustRule("inner", `tool@(\d+\.\d+\.\d+)`, 1),
		},
		unrelated: []string{"sarif v2.1.0", "example/tool@v1.2.0"},
	}

	_, err := tgt.scan(readFixture(t, "repo", "docs.md"))
	if err == nil {
		t.Fatal("scan accepted two rules matching overlapping text")
	}
	if want := "match overlapping text"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestBumpRejectsGoingBackwards(t *testing.T) {
	root := copyFixture(t)
	err := Bump(root, fixtureTargets(), mustParse(t, "1.1.9"), io.Discard)
	if err == nil {
		t.Fatal("Bump accepted a version older than the one in the repository")
	}
	if want := "must not go backwards"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestBumpWritesNothingWhenAnyFileFails(t *testing.T) {
	root := copyFixture(t)
	broken := filepath.Join(root, "page.html")
	content, err := os.ReadFile(broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, append(content, "<p>see 9.9.9</p>\n"...), 0666); err != nil {
		t.Fatal(err)
	}

	if err := Bump(root, fixtureTargets(), mustParse(t, "2.0.0"), io.Discard); err == nil {
		t.Fatal("Bump succeeded although one file was invalid")
	}

	for _, name := range []string{"docs.md", "download.bash"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if want := readFixture(t, "repo", name); !bytes.Equal(got, want) {
			t.Errorf("%s was modified although the bump failed:\n%s", name, got)
		}
	}
}

func TestBumpReportsMissingFile(t *testing.T) {
	root := copyFixture(t)
	if err := os.Remove(filepath.Join(root, "page.html")); err != nil {
		t.Fatal(err)
	}
	err := Bump(root, fixtureTargets(), mustParse(t, "2.0.0"), io.Discard)
	if err == nil {
		t.Fatal("Bump succeeded although a declared file is missing")
	}
	if want := "could not read page.html"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestCheckReportsEveryReference(t *testing.T) {
	root := copyFixture(t)
	var out bytes.Buffer
	if err := Check(root, fixtureTargets(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"docs.md:4: package reference: 1.2.0",
		"docs.md:9: action reference: 1.2.0",
		"download.bash:5: default version: 1.2.0",
		"page.html:3: version badge: 1.2.0",
		"version 1.2.0 is referenced 4 time(s)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, out.String())
		}
	}
}

func TestDeclaredTargetsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tgt := range targets {
		if tgt.path == "" {
			t.Error("a target has an empty path")
		}
		if seen[tgt.path] {
			t.Errorf("target %q is declared twice", tgt.path)
		}
		seen[tgt.path] = true
		if len(tgt.rules) == 0 {
			t.Errorf("target %q declares no rule", tgt.path)
		}
		for _, r := range tgt.rules {
			if r.pattern.NumSubexp() != 1 {
				t.Errorf("rule %q of %q captures %d groups, want 1", r.desc, tgt.path, r.pattern.NumSubexp())
			}
			if r.count < 1 {
				t.Errorf("rule %q of %q expects %d occurrences, want at least 1", r.desc, tgt.path, r.count)
			}
		}
	}
}

func TestDeclaredTargetsMatchRepository(t *testing.T) {
	if err := Check(filepath.Join("..", ".."), targets, io.Discard); err != nil {
		t.Fatalf("the declared version references are out of sync with the repository: %v", err)
	}
}

func TestDeclaredTargetsAcceptCRLF(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, tgt := range targets {
		content, err := os.ReadFile(filepath.Join(root, tgt.path))
		if err != nil {
			t.Fatal(err)
		}
		lf := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
		if _, err := tgt.scan(crlf); err != nil {
			t.Errorf("%s with CRLF line endings: %v", tgt.path, err)
		}
	}
}

func TestBumpRestoresFilesWhenWriteFails(t *testing.T) {
	root := copyFixture(t)
	locked := filepath.Join(root, "page.html")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}

	err := Bump(root, fixtureTargets(), mustParse(t, "2.0.0"), io.Discard)
	if err == nil {
		t.Fatal("Bump succeeded although one file was not writable")
	}
	if want := "the previously updated files were restored"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}

	for _, name := range []string{"docs.md", "download.bash"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if want := readFixture(t, "repo", name); !bytes.Equal(got, want) {
			t.Errorf("%s was left modified after the failed bump:\n%s", name, got)
		}
	}
}

func gitRepo(t *testing.T) *repo {
	t.Helper()
	dir := t.TempDir()
	r := &repo{ctx: t.Context(), root: dir, out: io.Discard}
	run := func(args ...string) {
		t.Helper()
		if _, err := r.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--initial-branch=master")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")
	run("config", "tag.gpgsign", "false")
	run("config", "tag.forceSignAnnotated", "false")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-m", "init")

	remote := t.TempDir()
	if _, err := (&repo{ctx: t.Context(), root: remote, out: io.Discard}).git("init", "--bare"); err != nil {
		t.Fatal(err)
	}
	run("remote", "add", "origin", remote)
	return r
}

func TestPreflightPassesOnCleanRepo(t *testing.T) {
	r := gitRepo(t)
	if err := r.preflight("v9.9.9"); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightRejectsUntrackedFile(t *testing.T) {
	r := gitRepo(t)
	if err := os.WriteFile(filepath.Join(r.root, "scratch.txt"), []byte("x\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	err := r.preflight("v9.9.9")
	if err == nil {
		t.Fatal("preflight accepted a working tree with an untracked file")
	}
	if want := "not clean"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestPreflightRejectsRemoteOnlyTag(t *testing.T) {
	r := gitRepo(t)
	for _, args := range [][]string{
		{"tag", "-m", "v9.9.9", "v9.9.9"},
		{"push", "origin", "v9.9.9"},
		{"tag", "-d", "v9.9.9"},
	} {
		if _, err := r.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	err := r.preflight("v9.9.9")
	if err == nil {
		t.Fatal("preflight accepted a tag which already exists on origin")
	}
	if want := "already exists on origin"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func changelogRoot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, changelogFile), []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCheckChangelogAcceptsUnreleasedEntries(t *testing.T) {
	root := changelogRoot(t, `<a id="unreleased"></a>
# Unreleased

- Report ShellCheck findings at their source locations.

<a id="v1.10.0"></a>
# [v1.10.0](https://github.com/kjanat/actionlint/releases/tag/v1.10.0) - 2026-08-19

- Move the module.

[Changes][v1.10.0]

[v1.10.0]: https://github.com/kjanat/actionlint/compare/v1.9.0...v1.10.0
`)
	if err := checkChangelog(root); err != nil {
		t.Fatal(err)
	}
}

func TestChangelogEntriesIgnoresEmptyBullets(t *testing.T) {
	entries, found := changelogEntries([]byte("# Unreleased\n\n-\n-   \n- Real entry.\n\n# [v1.10.0](x) - 2026-08-19\n\n- Not this one.\n"))
	if !found {
		t.Fatal("changelogEntries did not find the Unreleased heading")
	}
	if len(entries) != 1 || entries[0] != "- Real entry." {
		t.Errorf("entries are %q but only the real entry was expected", entries)
	}
}

func TestCheckChangelogRejectsMissingHeading(t *testing.T) {
	err := checkChangelog(changelogRoot(t, "# [v1.10.0](https://example.com) - 2026-08-19\n\n- Move the module.\n"))
	if err == nil {
		t.Fatal("checkChangelog accepted a changelog without an Unreleased heading")
	}
	if want := "has no"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestCheckChangelogReportsMissingFile(t *testing.T) {
	err := checkChangelog(t.TempDir())
	if err == nil {
		t.Fatal("checkChangelog accepted a repository without a changelog")
	}
	if want := "could not read CHANGELOG.md"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func TestRepositoryChangelogIsReleasable(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", changelogFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := changelogEntries(content); !found {
		t.Errorf("%s lost its %q heading, so no release can describe itself", changelogFile, unreleasedHeading)
	}
	if err := checkChangelogSections(content); err != nil {
		t.Error(err)
	}
}

const oneChangelogSection = `<a id="unreleased"></a>

# Unreleased

- Pending.

<a id="v1.11.0"></a>

# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.11.0) - 2026-08-20

- Release the thing.

[Changes][v1.11.0]

[v1.11.0]: https://github.com/kjanat/actionlint/compare/v1.10.0...v1.11.0
`

func TestCheckChangelogSectionsAcceptsCompleteSection(t *testing.T) {
	if err := checkChangelogSections([]byte(oneChangelogSection)); err != nil {
		t.Fatal(err)
	}
}

func TestSectionizeChangelogCreatesSection(t *testing.T) {
	content, err := sectionizeChangelog([]byte(oneChangelogSection), "v1.12.0", "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkChangelogSections(content); err != nil {
		t.Fatal(err)
	}
	notes, err := sectionNotes(content, "v1.12.0")
	if err != nil {
		t.Fatal(err)
	}
	if notes != "- Pending.\n" {
		t.Fatalf("the new section holds %q", notes)
	}
	if entries, _ := changelogEntries(content); len(entries) != 0 {
		t.Fatalf("the Unreleased heading still lists %v", entries)
	}
	text := string(content)
	if !strings.Contains(text, "## [v1.12.0](https://github.com/kjanat/actionlint/releases/tag/v1.12.0) - 2026-08-28") {
		t.Fatalf("the section heading is missing:\n%s", text)
	}
	if !strings.Contains(text, "[v1.12.0]: https://github.com/kjanat/actionlint/compare/v1.11.0...v1.12.0\n[v1.11.0]:") {
		t.Fatalf("the link definition is missing or misplaced:\n%s", text)
	}
}

func TestSectionizeChangelogKeepsExistingSection(t *testing.T) {
	content, err := sectionizeChangelog([]byte(oneChangelogSection), "v1.11.0", "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != oneChangelogSection {
		t.Fatalf("the changelog changed:\n%s", content)
	}
}

func TestSectionizeChangelogRejectsEmptyUnreleased(t *testing.T) {
	content := strings.Replace(oneChangelogSection, "- Pending.\n", "", 1)
	if _, err := sectionizeChangelog([]byte(content), "v1.12.0", "2026-08-28"); err == nil {
		t.Fatal("an empty Unreleased heading was accepted")
	}
}

func TestSectionizeChangelogFirstRelease(t *testing.T) {
	first := "<a id=\"unreleased\"></a>\n\n# Unreleased\n\n- First.\n"
	content, err := sectionizeChangelog([]byte(first), "v1.0.0", "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkChangelogSections(content); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "[v1.0.0]: https://github.com/kjanat/actionlint/tree/v1.0.0") {
		t.Fatalf("the first release link definition is missing:\n%s", content)
	}
}

func TestCheckChangelogSectionsRejectsIncompleteSection(t *testing.T) {
	for name, drop := range map[string]string{
		"heading":         "# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.11.0) - 2026-08-20",
		"changes link":    "[Changes][v1.11.0]",
		"link definition": "[v1.11.0]: https://github.com/kjanat/actionlint/compare/v1.10.0...v1.11.0",
	} {
		t.Run(name, func(t *testing.T) {
			content := strings.Replace(oneChangelogSection, drop, "", 1)
			err := checkChangelogSections([]byte(content))
			if err == nil {
				t.Fatalf("checkChangelogSections accepted a section without its %s", name)
			}
			if want := "v1.11.0"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		})
	}
}

func TestSectionNotesPrefersTheVersionSection(t *testing.T) {
	notes, err := sectionNotes([]byte(oneChangelogSection), "v1.11.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "- Release the thing.\n"; notes != want {
		t.Errorf("notes are %q but %q was expected", notes, want)
	}
}

func TestSectionNotesFallsBackToUnreleased(t *testing.T) {
	notes, err := sectionNotes([]byte(oneChangelogSection), "v1.12.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "- Pending.\n"; notes != want {
		t.Errorf("notes are %q but %q was expected", notes, want)
	}
}

func TestSectionNotesOfRepositoryChangelog(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", changelogFile))
	if err != nil {
		t.Fatal(err)
	}
	notes, err := sectionNotes(content, "v1.11.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes, "os.Root") {
		t.Errorf("the v1.11.0 notes lost their entries:\n%s", notes)
	}
	if strings.Contains(notes, "[Changes]") || strings.Contains(notes, "releases/tag/") {
		t.Errorf("the v1.11.0 notes carry the section wrapper:\n%s", notes)
	}
}

func TestRepositoryChangelogAcceptsCRLF(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", changelogFile))
	if err != nil {
		t.Fatal(err)
	}
	lf := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))

	if err := checkChangelogSections(crlf); err != nil {
		t.Errorf("%s with CRLF line endings: %v", changelogFile, err)
	}
	notes, err := sectionNotes(crlf, "v1.11.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(notes, "\r") {
		t.Errorf("the v1.11.0 notes keep their carriage returns: %q", notes)
	}
}

func TestChangelogReleaseRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		version string
		want    string
	}{
		"neither section nor entries": {
			strings.Replace(oneChangelogSection, "- Pending.\n", "", 1),
			"1.12.0",
			"lists no entries",
		},
		"section without entries": {
			strings.Replace(oneChangelogSection, "- Release the thing.\n", "", 1),
			"1.11.0",
			"describes no change",
		},
		"broken link definition": {
			strings.Replace(oneChangelogSection, "compare/v1.10.0...v1.11.0", "compare/v1.9.0...v1.10.0", 1),
			"1.11.0",
			"does not end at v1.11.0",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := changelogRelease([]byte(tc.content), mustParse(t, tc.version))
			if err == nil {
				t.Fatal("changelogRelease accepted the changelog")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestCheckChangelogSectionsRejectsBrokenSection(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"heading of another release": {
			"# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.11.0)",
			"# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.10.0)",
			"links to the release page of v1.10.0",
		},
		"changes link of another release": {
			"[Changes][v1.11.0]",
			"[Changes][v1.10.0]",
			"links its changes to v1.10.0",
		},
		"link definition of another release": {
			"[v1.11.0]: https://github.com/kjanat/actionlint/compare/v1.10.0...v1.11.0",
			"[v1.10.0]: https://github.com/kjanat/actionlint/compare/v1.9.0...v1.10.0",
			"declares no v1.10.0 section",
		},
		"link definition pointing elsewhere": {
			"compare/v1.10.0...v1.11.0",
			"compare/v1.9.0...v1.10.0",
			"does not end at v1.11.0",
		},
		"section without entries": {
			"- Release the thing.\n",
			"",
			"the v1.11.0 section describes no change",
		},
		"duplicate section": {
			`<a id="v1.11.0"></a>`,
			"<a id=\"v1.11.0\"></a>\n\n<a id=\"v1.11.0\"></a>",
			"declares the v1.11.0 section twice",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkChangelogSections([]byte(strings.Replace(oneChangelogSection, tc.from, tc.to, 1)))
			if err == nil {
				t.Fatal("checkChangelogSections accepted a broken changelog")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
