// Package outputformat defines the diagnostic output formats shared by the CLI and configuration.
package outputformat

import "slices"

const (
	Text    = "text"
	Pretty  = "pretty"
	Oneline = "oneline"
	GCC     = "gcc"
	JSON    = "json"
	JSONL   = "jsonl"
	SARIF   = "sarif"
	GitHub  = "github"
)

var names = []string{Text, Pretty, Oneline, GCC, JSON, JSONL, SARIF, GitHub}

// Names returns the supported output formats in display order.
func Names() []string { return slices.Clone(names) }

// Valid reports whether name is a supported output format.
func Valid(name string) bool { return slices.Contains(names, name) }
