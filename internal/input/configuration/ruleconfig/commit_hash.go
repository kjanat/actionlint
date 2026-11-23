package ruleconfig

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"

	"github.com/bmatcuk/doublestar/v4"
)

// CommitHashOptions excludes nothing in its zero value.
type CommitHashOptions struct {
	// exclude holds validated doublestar patterns.
	exclude []string
}

type commitHashWire struct {
	Exclude []string `yaml:"exclude"`
}

// NewCommitHashOptions accepts repository, action-path and docker://image
// globs, and an @ref restricts an exception to that ref.
func NewCommitHashOptions(exclude ...string) (*CommitHashOptions, error) {
	for _, pattern := range exclude {
		if pattern == "" || !doublestar.ValidatePattern(pattern) {
			return nil, fmt.Errorf("invalid require-commit-hash exclusion %q", pattern)
		}
	}
	return &CommitHashOptions{exclude: slices.Clone(exclude)}, nil
}

func (o *CommitHashOptions) DecodeConfig(value any) error {
	var wire commitHashWire
	if err := configdecode.Decode(value, &wire); err != nil {
		return err
	}
	next, err := NewCommitHashOptions(wire.Exclude...)
	if err != nil {
		return err
	}
	*o = *next
	return nil
}

func (o *CommitHashOptions) EncodeConfig() (any, error) {
	if o == nil {
		return commitHashWire{}, nil
	}
	return commitHashWire{Exclude: slices.Clone(o.exclude)}, nil
}

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
