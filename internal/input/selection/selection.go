// Package selection implements shared repository-relative file selection for features.
package selection

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"

	"github.com/bmatcuk/doublestar/v4"
)

// PathSelection with nil includes selects everything, with empty includes selects nothing, and exclusions always win.
type PathSelection struct {
	includes []string
	excludes []string
}

type selectionDocument struct {
	Includes []string `yaml:"includes,omitempty"`
	Excludes []string `yaml:"excludes,omitempty"`
}

func NewPathSelection(includes, excludes []string) (PathSelection, error) {
	if err := validatePatterns(includes, excludes); err != nil {
		return PathSelection{}, err
	}
	return PathSelection{includes: slices.Clone(includes), excludes: slices.Clone(excludes)}, nil
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

func (s PathSelection) IsZero() bool { return s.includes == nil && s.excludes == nil }

func ValidPattern(pattern string) bool {
	p := strings.TrimPrefix(pattern, "!")
	// filepath.IsAbs("/tmp/*") is false on Windows, where a volume is required.
	return p != "" && !strings.ContainsAny(p, "\\:") && !strings.HasPrefix(p, "/") && !strings.Contains("/"+p+"/", "/../") && doublestar.ValidatePattern(p)
}

func (s *PathSelection) DecodeConfig(value any) error {
	if value == nil {
		*s = PathSelection{}
		return nil
	}
	var next selectionDocument
	if err := configdecode.Decode(value, &next); err != nil {
		return err
	}
	if err := validatePatterns(next.Includes, next.Excludes); err != nil {
		return err
	}
	*s = PathSelection{includes: next.Includes, excludes: next.Excludes}
	return nil
}

// EncodeConfig preserves an explicit empty includes list across round trips.
func (s PathSelection) EncodeConfig() (any, error) {
	v := map[string]any{}
	if s.includes != nil {
		v["includes"] = slices.Clone(s.includes)
	}
	if s.excludes != nil {
		v["excludes"] = slices.Clone(s.excludes)
	}
	return v, nil
}

func (s PathSelection) Match(path string) bool {
	return MatchPatterns(s.includes, s.excludes, path)
}

// Excludes reports only explicit exclusions, including negated include patterns.
func (s PathSelection) Excludes(path string) bool {
	return !matchPatterns(s.includes, s.excludes, path, true)
}

// MatchPatterns expects validated patterns, and a malformed pattern never matches.
func MatchPatterns(includes, excludes []string, path string) bool {
	return matchPatterns(includes, excludes, path, includes == nil)
}

func matchPatterns(includes, excludes []string, path string, match bool) bool {
	path = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
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

// CopyConfig shares immutable selection patterns.
func (s PathSelection) CopyConfig() (any, error) { return s, nil }
