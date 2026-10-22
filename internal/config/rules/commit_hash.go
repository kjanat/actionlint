package rules

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

// CommitHashOptions selects exceptions to immutable action references.
type CommitHashOptions struct {
	// Exclude contains repository, action-path, or docker://image glob patterns.
	// Repository patterns such as my-org/* also cover actions and workflows within
	// matching repositories. Include @ref to restrict an exception to that ref.
	Exclude []string `yaml:"exclude" jsonschema:"nullable"`
}

func (o *CommitHashOptions) UnmarshalYAML(n *yaml.Node) error {
	type plain CommitHashOptions
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for _, pattern := range next.Exclude {
		if pattern == "" || !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("invalid require-commit-hash exclusion %q", pattern)
		}
	}
	*o = CommitHashOptions(next)
	return nil
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
	for _, pattern := range o.Exclude {
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
