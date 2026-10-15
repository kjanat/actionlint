// Package workflowcommands checks commands written to the Actions runner log.
package workflowcommands

import "regexp"

var deprecated = regexp.MustCompile(`(?:::(save-state|set-output|set-env)\s+name=[a-zA-Z][a-zA-Z_-]*::\S+|::(add-path)::\S+)`)

// Finding identifies the command name by a half-open byte range in script text.
type Finding struct {
	Start, End           int
	Command, Replacement string
}

// Deprecated returns findings without retaining state or requiring a syntax tree.
func Deprecated(script string) []Finding {
	matches := deprecated.FindAllStringSubmatchIndex(script, -1)
	findings := make([]Finding, 0, len(matches))
	for _, match := range matches {
		start, end := match[2], match[3]
		if start < 0 {
			start, end = match[4], match[5]
		}
		command := script[start:end]
		var replacement string
		switch command {
		case "set-output":
			replacement = `echo "{name}={value}" >> $GITHUB_OUTPUT`
		case "save-state":
			replacement = `echo "{name}={value}" >> $GITHUB_STATE`
		case "set-env":
			replacement = `echo "{name}={value}" >> $GITHUB_ENV`
		case "add-path":
			replacement = `echo "{path}" >> $GITHUB_PATH`
		}
		findings = append(findings, Finding{Start: start, End: end, Command: command, Replacement: replacement})
	}
	return findings
}
