package githubaction

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev"
)

func quotedIgnoreHints(patterns []string, problems []*actionlint.Error) []string {
	var hints []string
	for i, pattern := range patterns {
		if len(pattern) < 2 || (pattern[0] != '\'' && pattern[0] != '"') || pattern[len(pattern)-1] != pattern[0] {
			continue
		}
		unquoted := pattern[1 : len(pattern)-1]
		regex, err := regexp.Compile(unquoted)
		if err != nil {
			continue
		}
		for _, problem := range problems {
			if regex.MatchString(problem.Message) {
				hints = append(hints, fmt.Sprintf("Input 'ignore' pattern %d contains literal surrounding quotes. Removing them would match a remaining diagnostic. Inside ignore: |, use: %s", i+1, unquoted))
				break
			}
		}
	}
	return hints
}

func (a *action) emitConfiguration(result *lintResult, workingDir string) {
	slices.SortFunc(result.configs, func(a, b actionlint.ConfigReport) int {
		return strings.Compare(a.Project, b.Project)
	})
	for _, config := range result.configs {
		source := "defaults (no config file found)"
		if config.File != "" {
			source = config.File
			if rel, err := filepath.Rel(workingDir, config.File); err == nil {
				source = filepath.ToSlash(rel)
			}
			if config.Explicit {
				source += " (config-file input)"
			} else {
				source += " (automatically discovered)"
			}
		}
		if len(config.Overrides) > 0 {
			source += "; overrides: " + strings.Join(config.Overrides, ", ")
		}
		_, _ = fmt.Fprintf(a.stdout, "Configuration: %s\n", commandEscape(source))
	}
	for _, hint := range result.hints {
		_, _ = fmt.Fprintf(a.stdout, "::warning title=Check ignore input::%s\n", commandEscape(hint))
	}
}
