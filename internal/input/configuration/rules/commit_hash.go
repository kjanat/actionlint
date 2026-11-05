package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kjanat/go-yaml/v4"
)

// CommitHashOptions selects exceptions to immutable action references. The
// zero value excludes nothing.
type CommitHashOptions struct {
	// exclude holds validated doublestar patterns. Excludes matches them
	// without validating again.
	exclude []string
}

// commitHashWire is the YAML form of CommitHashOptions.
type commitHashWire struct {
	Exclude []string `yaml:"exclude"`
}

// NewCommitHashOptions returns options that exclude references matching any of
// the patterns. A pattern is a repository, action-path, or docker://image glob.
// Repository patterns such as my-org/* also cover actions and workflows within
// matching repositories. Include @ref to restrict an exception to that ref.
// An empty or malformed pattern is an error.
func NewCommitHashOptions(exclude ...string) (*CommitHashOptions, error) {
	for _, pattern := range exclude {
		if pattern == "" || !doublestar.ValidatePattern(pattern) {
			return nil, fmt.Errorf("invalid require-commit-hash exclusion %q", pattern)
		}
	}
	return &CommitHashOptions{exclude: slices.Clone(exclude)}, nil
}

// UnmarshalYAML reads the exclude list and rejects malformed patterns.
func (o *CommitHashOptions) UnmarshalYAML(n *yaml.Node) error {
	var wire commitHashWire
	if err := n.Load(&wire, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	next, err := NewCommitHashOptions(wire.Exclude...)
	if err != nil {
		return err
	}
	*o = *next
	return nil
}

// MarshalYAML writes the exclude list.
func (o *CommitHashOptions) MarshalYAML() (any, error) {
	if o == nil {
		return commitHashWire{}, nil
	}
	return commitHashWire{Exclude: slices.Clone(o.exclude)}, nil
}

// Excludes matches the reference without changing how any other rule checks it.
func (o *CommitHashOptions) Excludes(spec string) bool {
	if o == nil {
		return false
	}
	name, _, _ := strings.Cut(spec, "@")
	repository := name
	if !strings.HasPrefix(name, "docker://") {
		parts := strings.SplitN(name, "/", 3)
		if len(parts) == 3 {
			repository = parts[0] + "/" + parts[1]
		}
	}
	for _, pattern := range o.exclude {
		if strings.Contains(pattern, "@") {
			if doublestar.MatchUnvalidated(pattern, spec) {
				return true
			}
		} else if doublestar.MatchUnvalidated(pattern, name) || doublestar.MatchUnvalidated(pattern, repository) {
			return true
		}
	}
	return false
}
