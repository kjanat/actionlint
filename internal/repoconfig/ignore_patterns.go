package repoconfig

import (
	"fmt"
	"regexp"
)

// CompileIgnorePatterns validates message filters used by analysis and config initialization.
func CompileIgnorePatterns(patterns []string) (IgnorePatterns, error) {
	ignore := make(IgnorePatterns, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression for ignore pattern %q: %w", pattern, err)
		}
		ignore = append(ignore, re)
	}
	return ignore, nil
}
