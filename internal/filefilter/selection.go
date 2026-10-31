// Package filefilter implements shared repository-relative file selection for features.
package filefilter

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kjanat/go-yaml/v4"
)

// Selection is a feature-independent file scope over validated patterns. Nil
// includes selects everything; an explicit empty list selects nothing.
// Exclusions always win. The zero value selects everything.
type Selection struct {
	// includes selects repository-relative files using doublestar globs; ! excludes.
	includes []string
	// excludes removes matching files, regardless of include order.
	excludes []string
}

// selectionDocument is the YAML wire form of a Selection.
type selectionDocument struct {
	Includes []string `yaml:"includes,omitempty"`
	Excludes []string `yaml:"excludes,omitempty"`
}

// NewSelection validates both pattern lists and owns copies of them. A nil
// includes list selects everything; an empty one selects nothing.
func NewSelection(includes, excludes []string) (Selection, error) {
	if err := validatePatterns(includes, excludes); err != nil {
		return Selection{}, err
	}
	return Selection{includes: slices.Clone(includes), excludes: slices.Clone(excludes)}, nil
}

func validatePatterns(lists ...[]string) error {
	for _, patterns := range lists {
		for _, p := range patterns {
			if !ValidPattern(p) {
				return fmt.Errorf("invalid repository-relative file glob %q", p)
			}
		}
	}
	return nil
}

// IsZero distinguishes no selection from an explicitly empty includes list.
func (s Selection) IsZero() bool { return s.includes == nil && s.excludes == nil }

// ValidPattern rejects ambiguous or escaping file patterns.
func ValidPattern(pattern string) bool {
	p := strings.TrimPrefix(pattern, "!")
	// Globs always use repository-relative slash paths, independent of the host.
	// filepath.IsAbs("/tmp/*") is false on Windows, where a volume is required.
	return p != "" && !strings.ContainsAny(p, "\\:") && !strings.HasPrefix(p, "/") && !strings.Contains("/"+p+"/", "/../") && doublestar.ValidatePattern(p)
}

// UnmarshalYAML validates the whole scope and rejects unknown keys.
func (s *Selection) UnmarshalYAML(n *yaml.Node) error {
	var next selectionDocument
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if err := validatePatterns(next.Includes, next.Excludes); err != nil {
		return err
	}
	for i := 0; i < len(n.Content); i += 2 {
		for _, p := range n.Content[i+1].Content {
			if p.ShortTag() != "!!str" {
				return errors.New("file patterns must be strings")
			}
		}
	}
	*s = Selection{includes: next.Includes, excludes: next.Excludes}
	return nil
}

// MarshalYAML preserves an explicit empty includes list across round trips.
func (s Selection) MarshalYAML() (any, error) {
	v := map[string]any{}
	if s.includes != nil {
		v["includes"] = slices.Clone(s.includes)
	}
	if s.excludes != nil {
		v["excludes"] = slices.Clone(s.excludes)
	}
	return v, nil
}

// Match evaluates a normalized path. A rule cannot re-include an excluded file.
func (s Selection) Match(path string) bool {
	return MatchPatterns(s.includes, s.excludes, path)
}

// MatchPatterns evaluates a path against include and exclude globs with
// Selection semantics. Callers validate patterns at their input boundary;
// a malformed pattern never matches.
func MatchPatterns(includes, excludes []string, path string) bool {
	path = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
	match := includes == nil
	for _, pattern := range includes {
		found, _ := doublestar.Match(normalizePattern(pattern), path)
		if found && strings.HasPrefix(pattern, "!") {
			return false
		}
		match = match || found
	}
	for _, pattern := range excludes {
		if found, _ := doublestar.Match(normalizePattern(pattern), path); found {
			return false
		}
	}
	return match
}

func normalizePattern(pattern string) string {
	return path.Clean(strings.TrimPrefix(pattern, "!"))
}
