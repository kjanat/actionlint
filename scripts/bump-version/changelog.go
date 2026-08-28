package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	changelogFile     = "CHANGELOG.md"
	unreleasedHeading = "# Unreleased"
)

var (
	changelogAnchor  = regexp.MustCompile(`(?m)^<a id="(v\d+\.\d+\.\d+)"></a>\r?$`)
	changelogHeading = regexp.MustCompile(`(?m)^#{1,2} \[(v\d+\.\d+\.\d+)\]\((https://\S+/releases/tag/(v\d+\.\d+\.\d+))\) - \d{4}-\d{2}-\d{2}\r?$`)
	changelogChanges = regexp.MustCompile(`(?m)^\[Changes\]\[(v\d+\.\d+\.\d+)\]\r?$`)
	changelogLink    = regexp.MustCompile(`(?m)^\[(v\d+\.\d+\.\d+)\]: (https://\S+)\r?$`)
	changelogEntry   = regexp.MustCompile(`(?m)^- \S`)
)

type section struct {
	version string
	body    []byte
}

// sections splits the changelog at its version anchors. The link definition block at the end of the
// file belongs to no section.
func sections(content []byte) []section {
	anchors := changelogAnchor.FindAllSubmatchIndex(content, -1)
	links := changelogLink.FindIndex(content)

	found := make([]section, 0, len(anchors))
	for i, a := range anchors {
		end := len(content)
		if i+1 < len(anchors) {
			end = anchors[i+1][0]
		}
		if links != nil && links[0] < end && links[0] > a[1] {
			end = links[0]
		}
		found = append(found, section{version: string(content[a[2]:a[3]]), body: content[a[1]:end]})
	}
	return found
}

func checkSection(s section) error {
	heading := changelogHeading.FindSubmatch(s.body)
	if heading == nil {
		return fmt.Errorf("the %s section has no release page heading, so its release notes cannot be reached", s.version)
	}
	if titled, linked := string(heading[1]), string(heading[3]); titled != s.version || linked != s.version {
		return fmt.Errorf("the %s section is titled %s and links to the release page of %s", s.version, titled, linked)
	}
	if !changelogEntry.Match(s.body) {
		return fmt.Errorf("the %s section describes no change", s.version)
	}
	changes := changelogChanges.FindSubmatch(s.body)
	if changes == nil {
		return fmt.Errorf("the %s section has no [Changes] link", s.version)
	}
	if linked := string(changes[1]); linked != s.version {
		return fmt.Errorf("the %s section links its changes to %s", s.version, linked)
	}
	return nil
}

func checkChangelogSections(content []byte) error {
	found := sections(content)
	if len(found) == 0 {
		return fmt.Errorf("%s declares no version section", changelogFile)
	}

	seen := map[string]bool{}
	for _, s := range found {
		if seen[s.version] {
			return fmt.Errorf("%s declares the %s section twice", changelogFile, s.version)
		}
		seen[s.version] = true
	}
	for _, s := range found {
		if err := checkSection(s); err != nil {
			return fmt.Errorf("%s: %w", changelogFile, err)
		}
	}

	defined := map[string]bool{}
	for _, m := range changelogLink.FindAllSubmatch(content, -1) {
		v, url := string(m[1]), string(m[2])
		if defined[v] {
			return fmt.Errorf("%s defines the %s link twice", changelogFile, v)
		}
		defined[v] = true
		if !seen[v] {
			return fmt.Errorf("%s defines the %s link but declares no %s section", changelogFile, v, v)
		}
		if !strings.HasSuffix(url, "/"+v) && !strings.HasSuffix(url, "..."+v) {
			return fmt.Errorf("%s: the %s link points at %s, which does not end at %s", changelogFile, v, url, v)
		}
	}
	for _, s := range found {
		if !defined[s.version] {
			return fmt.Errorf("%s: the %s section has no link definition, so its [Changes] link resolves to nothing", changelogFile, s.version)
		}
	}
	return nil
}

func changelogEntries(content []byte) ([]string, bool) {
	s := bufio.NewScanner(bytes.NewReader(content))
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	found := false
	var entries []string
	for s.Scan() {
		line := strings.TrimRight(s.Text(), " \t")
		if !found {
			found = line == unreleasedHeading
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if changelogEntry.MatchString(line) {
			entries = append(entries, line)
		}
	}
	return entries, found
}

func hasSection(content []byte, tag string) bool {
	return bytes.Contains(content, fmt.Appendf(nil, "<a id=%q></a>", tag))
}

// sectionNotes resolves the release notes of tag: its own section when the changelog has one, and
// the Unreleased entries when it does not.
func sectionNotes(content []byte, tag string) (string, error) {
	if err := checkChangelogSections(content); err != nil {
		return "", err
	}
	for _, s := range sections(content) {
		if s.version != tag {
			continue
		}
		notes := s.body
		if heading := changelogHeading.FindIndex(notes); heading != nil {
			notes = notes[heading[1]:]
		}
		if changes := changelogChanges.FindIndex(notes); changes != nil {
			notes = notes[:changes[0]]
		}
		return strings.TrimSpace(strings.ReplaceAll(string(notes), "\r\n", "\n")) + "\n", nil
	}

	entries, found := changelogEntries(content)
	if !found {
		return "", fmt.Errorf("%s has no %s section and no %q heading, so the release notes for %s cannot be located", changelogFile, tag, unreleasedHeading, tag)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("%s has no %s section and lists no entries under %q, so %s would be released without notes", changelogFile, tag, unreleasedHeading, tag)
	}
	return strings.Join(entries, "\n") + "\n", nil
}

func changelogRelease(content []byte, v version) error {
	_, err := sectionNotes(content, "v"+v.String())
	return err
}

const changelogRepoURL = "https://github.com/kjanat/actionlint"

var changelogUnreleased = regexp.MustCompile(`(?m)^` + unreleasedHeading + `[ \t]*\r?$`)

// sectionizeChangelog moves the Unreleased entries into a new section for tag dated date, leaving
// the Unreleased heading empty. Content already holding a section for tag is returned unchanged.
func sectionizeChangelog(content []byte, tag, date string) ([]byte, error) {
	if hasSection(content, tag) {
		return content, nil
	}

	heading := changelogUnreleased.FindIndex(content)
	if heading == nil {
		return nil, fmt.Errorf("%s has no %q heading, so no entries can move into the %s section", changelogFile, unreleasedHeading, tag)
	}
	end := len(content)
	if anchor := changelogAnchor.FindIndex(content[heading[1]:]); anchor != nil {
		end = heading[1] + anchor[0]
	} else if links := changelogLink.FindIndex(content[heading[1]:]); links != nil {
		end = heading[1] + links[0]
	}
	entries := bytes.TrimSpace(content[heading[1]:end])
	if !changelogEntry.Match(entries) {
		return nil, fmt.Errorf("%s lists no entries under %q, so the %s section would describe no change", changelogFile, unreleasedHeading, tag)
	}

	previous := ""
	if found := sections(content); len(found) > 0 {
		previous = found[0].version
	}
	linkURL := changelogRepoURL + "/tree/" + tag
	if previous != "" {
		linkURL = changelogRepoURL + "/compare/" + previous + "..." + tag
	}

	var b bytes.Buffer
	b.Grow(len(content) + 512)
	b.Write(content[:heading[1]])
	fmt.Fprintf(&b, "\n\n<a id=%q></a>\n\n## [%s](%s/releases/tag/%s) - %s\n\n", tag, tag, changelogRepoURL, tag, date)
	b.Write(entries)
	fmt.Fprintf(&b, "\n\n[Changes][%s]\n\n", tag)
	rest := content[end:]
	definition := fmt.Appendf(nil, "[%s]: %s\n", tag, linkURL)
	if links := changelogLink.FindIndex(rest); links != nil {
		b.Write(rest[:links[0]])
		b.Write(definition)
		b.Write(rest[links[0]:])
	} else {
		b.Write(bytes.TrimLeft(rest, "\n"))
		b.WriteString("\n")
		b.Write(definition)
	}

	result := b.Bytes()
	if err := checkChangelogSections(result); err != nil {
		return nil, fmt.Errorf("the rewritten changelog does not validate: %w", err)
	}
	return result, nil
}

func SectionizeChangelog(root string, v version, date string, out io.Writer) error {
	content, err := readChangelog(root)
	if err != nil {
		return err
	}
	tag := "v" + v.String()
	updated, err := sectionizeChangelog(content, tag, date)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, content) {
		_, _ = fmt.Fprintf(out, "%s: the %s section already exists\n", changelogFile, tag)
		return nil
	}
	if err := os.WriteFile(filepath.Join(root, changelogFile), updated, 0666); err != nil {
		return fmt.Errorf("could not write %s: %w", changelogFile, err)
	}
	_, _ = fmt.Fprintf(out, "%s: the Unreleased entries moved into the %s section\n", changelogFile, tag)
	return nil
}

func readChangelog(root string) ([]byte, error) {
	content, err := os.ReadFile(filepath.Join(root, changelogFile))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", changelogFile, err)
	}
	return content, nil
}

func checkChangelog(root string) error {
	content, err := readChangelog(root)
	if err != nil {
		return err
	}
	if _, found := changelogEntries(content); !found {
		return fmt.Errorf("%s has no %q heading, so the release notes for this version cannot be located", changelogFile, unreleasedHeading)
	}
	return checkChangelogSections(content)
}

func CheckChangelogRelease(root string, v version, out io.Writer) error {
	content, err := readChangelog(root)
	if err != nil {
		return err
	}
	if err := changelogRelease(content, v); err != nil {
		return err
	}
	source := unreleasedHeading
	if tag := "v" + v.String(); hasSection(content, tag) {
		source = "the " + tag + " section"
	}
	_, _ = fmt.Fprintf(out, "%s: %s describes this release\n", changelogFile, source)
	return nil
}
