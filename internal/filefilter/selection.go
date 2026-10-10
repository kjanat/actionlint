// Package filefilter implements shared repository-relative file selection for features.
package filefilter

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

// Selection is a feature-independent file scope. Nil includes selects everything;
// an explicit empty list selects nothing. Exclusions always win.
type Selection struct {
	// Includes selects repository-relative files using doublestar globs; ! excludes.
	Includes []string `yaml:"includes,omitempty" jsonschema:"nullable"`
	// Excludes removes matching files, regardless of include order.
	Excludes []string `yaml:"excludes,omitempty" jsonschema:"nullable"`
}

// IsZero distinguishes no selection from an explicitly empty includes list.
func (s Selection) IsZero() bool { return s.Includes == nil && s.Excludes == nil }

// ValidPattern rejects ambiguous or escaping file patterns, not config paths.
func ValidPattern(pattern string) bool {
	p := strings.TrimPrefix(pattern, "!")
	// Globs always use repository-relative slash paths, independent of the host.
	// filepath.IsAbs("/tmp/*") is false on Windows, where a volume is required.
	return p != "" && !strings.ContainsAny(p, "\\:") && !strings.HasPrefix(p, "/") && !strings.Contains("/"+p+"/", "/../") && doublestar.ValidatePattern(p)
}

// UnmarshalYAML validates the whole scope and rejects unknown keys.
func (s *Selection) UnmarshalYAML(n *yaml.Node) error {
	type plain Selection
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for _, patterns := range [][]string{next.Includes, next.Excludes} {
		for _, p := range patterns {
			if !ValidPattern(p) {
				return fmt.Errorf("invalid repository-relative file glob %q", p)
			}
		}
	}
	for i := 0; i < len(n.Content); i += 2 {
		for _, p := range n.Content[i+1].Content {
			if p.ShortTag() != "!!str" {
				return errors.New("file patterns must be strings")
			}
		}
	}
	*s = Selection(next)
	return nil
}

// MarshalYAML preserves an explicit empty includes list across round trips.
func (s Selection) MarshalYAML() (any, error) {
	v := map[string]any{}
	if s.Includes != nil {
		v["includes"] = s.Includes
	}
	if s.Excludes != nil {
		v["excludes"] = s.Excludes
	}
	return v, nil
}

// Match evaluates a normalized path. A rule cannot re-include an excluded file.
func (s Selection) Match(path string) bool {
	path = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
	match := s.Includes == nil
	for _, pattern := range s.Includes {
		found, _ := doublestar.Match(strings.TrimPrefix(pattern, "!"), path)
		if found && strings.HasPrefix(pattern, "!") {
			return false
		}
		match = match || found
	}
	for _, pattern := range s.Excludes {
		if found, _ := doublestar.Match(strings.TrimPrefix(pattern, "!"), path); found {
			return false
		}
	}
	return match
}
