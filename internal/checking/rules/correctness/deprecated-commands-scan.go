package correctness

import "regexp"

var deprecatedCommandPattern = regexp.MustCompile(`(?:::(save-state|set-output|set-env)\s+name=[a-zA-Z][a-zA-Z_-]*::\S+|::(add-path)::\S+)`)

// DeprecatedCommandFinding identifies the command name by a half-open byte range in script text.
type DeprecatedCommandFinding struct {
	Start, End           int
	Command, Replacement string
}

// DeprecatedCommands finds deprecatedCommandPattern workflow commands in script text.
func DeprecatedCommands(script string) []DeprecatedCommandFinding {
	matches := deprecatedCommandPattern.FindAllStringSubmatchIndex(script, -1)
	findings := make([]DeprecatedCommandFinding, 0, len(matches))
	for _, match := range matches {
		start, end := match[2], match[3]
		if start < 0 {
			start, end = match[4], match[5]
		}
		command := script[start:end]
		var replacement string
		switch command {
		case "set-output":
			replacement = `echo "{name}={value}" >> "${GITHUB_OUTPUT}"`
		case "save-state":
			replacement = `echo "{name}={value}" >> "${GITHUB_STATE}"`
		case "set-env":
			replacement = `echo "{name}={value}" >> "${GITHUB_ENV}"`
		case "add-path":
			replacement = `echo "{path}" >> "${GITHUB_PATH}"`
		}
		findings = append(findings, DeprecatedCommandFinding{Start: start, End: end, Command: command, Replacement: replacement})
	}
	return findings
}
