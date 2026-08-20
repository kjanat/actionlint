package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	semverPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	anyVersion    = regexp.MustCompile(`\d+\.\d+\.\d+`)
)

type version struct {
	major, minor, patch int
}

func parseVersion(s string) (version, error) {
	m := semverPattern.FindStringSubmatch(s)
	if m == nil {
		return version{}, fmt.Errorf("version %q does not match `^\\d+\\.\\d+\\.\\d+$`", s)
	}
	nums := [3]int{}
	for i := range nums {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, fmt.Errorf("version %q has a number which is too large: %w", s, err)
		}
		nums[i] = n
	}
	return version{nums[0], nums[1], nums[2]}, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) compare(o version) int {
	for _, p := range [][2]int{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

type span struct {
	start, end int
}

func (s span) contains(o span) bool {
	return s.start <= o.start && o.end <= s.end
}

type occurrence struct {
	rule    string
	match   span
	value   span
	version version
}

func lineAt(content []byte, pos int) int {
	return bytes.Count(content[:pos], []byte{'\n'}) + 1
}

func lineTextAt(content []byte, pos int) string {
	start := bytes.LastIndexByte(content[:pos], '\n') + 1
	end := bytes.IndexByte(content[pos:], '\n')
	if end < 0 {
		end = len(content)
	} else {
		end += pos
	}
	return strings.TrimSpace(string(content[start:end]))
}

func allIndex(content []byte, literal string) []span {
	var spans []span
	lit := []byte(literal)
	for off := 0; ; {
		i := bytes.Index(content[off:], lit)
		if i < 0 {
			return spans
		}
		spans = append(spans, span{off + i, off + i + len(lit)})
		off += i + len(lit)
	}
}

func (t *target) scan(content []byte) ([]occurrence, error) {
	occs := make([]occurrence, 0, len(t.rules))
	covered := make([]span, 0, len(t.rules)+len(t.unrelated))

	for _, r := range t.rules {
		ms := r.pattern.FindAllSubmatchIndex(content, -1)
		if len(ms) != r.count {
			return nil, fmt.Errorf("%s: expected %d occurrence(s) of %s but found %d", t.path, r.count, r.desc, len(ms))
		}
		for _, m := range ms {
			v, err := parseVersion(string(content[m[2]:m[3]]))
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %s: %w", t.path, lineAt(content, m[2]), r.desc, err)
			}
			o := occurrence{rule: r.desc, match: span{m[0], m[1]}, value: span{m[2], m[3]}, version: v}
			occs = append(occs, o)
			covered = append(covered, o.match)
		}
	}

	sort.Slice(occs, func(i, j int) bool { return occs[i].match.start < occs[j].match.start })
	for i := 1; i < len(occs); i++ {
		if occs[i].match.start < occs[i-1].match.end {
			return nil, fmt.Errorf(
				"%s:%d: %s and %s match overlapping text, so the update would be ambiguous",
				t.path, lineAt(content, occs[i].match.start), occs[i-1].rule, occs[i].rule,
			)
		}
	}

	for _, lit := range t.unrelated {
		found := allIndex(content, lit)
		if len(found) == 0 {
			return nil, fmt.Errorf("%s: unrelated version reference %q no longer appears, so the declaration is stale", t.path, lit)
		}
		covered = append(covered, found...)
	}

	for _, m := range anyVersion.FindAllIndex(content, -1) {
		found := span{m[0], m[1]}
		known := false
		for _, c := range covered {
			if c.contains(found) {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf(
				"%s:%d: undeclared version reference %q in %q. declare it in scripts/bump-version/targets.go or list it as unrelated",
				t.path, lineAt(content, found.start), string(content[found.start:found.end]), lineTextAt(content, found.start),
			)
		}
	}

	return occs, nil
}

func rewrite(content []byte, occs []occurrence, v version) []byte {
	out := make([]byte, 0, len(content))
	last := 0
	for _, o := range occs {
		out = append(out, content[last:o.value.start]...)
		out = append(out, v.String()...)
		last = o.value.end
	}
	return append(out, content[last:]...)
}

func (t *target) verify(content []byte, v version) ([]occurrence, error) {
	occs, err := t.scan(content)
	if err != nil {
		return nil, err
	}
	for _, o := range occs {
		if o.version != v {
			return nil, fmt.Errorf(
				"%s:%d: %s refers to version %s but %s was expected",
				t.path, lineAt(content, o.value.start), o.rule, o.version, v,
			)
		}
	}
	return occs, nil
}

func (t *target) bump(content []byte, v version) ([]byte, int, error) {
	occs, err := t.scan(content)
	if err != nil {
		return nil, 0, err
	}
	for _, o := range occs {
		if o.version.compare(v) > 0 {
			return nil, 0, fmt.Errorf(
				"%s:%d: %s refers to version %s which is newer than %s. a version bump must not go backwards",
				t.path, lineAt(content, o.value.start), o.rule, o.version, v,
			)
		}
	}
	updated := rewrite(content, occs, v)
	if _, err := t.verify(updated, v); err != nil {
		return nil, 0, fmt.Errorf("the update did not produce a consistent file: %w", err)
	}
	return updated, len(occs), nil
}

func Check(root string, ts []*target, out io.Writer) error {
	found := map[string][]string{}
	for _, t := range ts {
		path := filepath.Join(root, t.path)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", t.path, err)
		}
		occs, err := t.scan(content)
		if err != nil {
			return err
		}
		for _, o := range occs {
			fmt.Fprintf(out, "%s:%d: %s: %s\n", t.path, lineAt(content, o.value.start), o.rule, o.version)
			key := o.version.String()
			found[key] = append(found[key], t.path)
		}
	}

	versions := make([]string, 0, len(found))
	for v := range found {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		fmt.Fprintf(out, "version %s is referenced %d time(s)\n", v, len(found[v]))
	}
	return nil
}

func Bump(root string, ts []*target, v version, out io.Writer) error {
	type update struct {
		path    string
		content []byte
		count   int
	}

	updates := make([]update, 0, len(ts))
	for _, t := range ts {
		path := filepath.Join(root, t.path)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", t.path, err)
		}
		updated, n, err := t.bump(content, v)
		if err != nil {
			return err
		}
		updates = append(updates, update{path, updated, n})
	}

	for _, u := range updates {
		if err := os.WriteFile(u.path, u.content, 0666); err != nil {
			return fmt.Errorf("could not write %s: %w", u.path, err)
		}
	}

	for i, t := range ts {
		content, err := os.ReadFile(updates[i].path)
		if err != nil {
			return fmt.Errorf("could not re-read %s: %w", t.path, err)
		}
		if _, err := t.verify(content, v); err != nil {
			return fmt.Errorf("the repository is inconsistent after the update: %w", err)
		}
		fmt.Fprintf(out, "%s: %d reference(s) set to %s\n", t.path, updates[i].count, v)
	}
	return nil
}

func paths(ts []*target) []string {
	ps := make([]string, 0, len(ts))
	for _, t := range ts {
		ps = append(ps, t.path)
	}
	return ps
}
