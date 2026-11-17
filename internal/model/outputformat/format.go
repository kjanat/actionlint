// Package outputformat names the built-in diagnostic representations. The
// configuration loader validates format names with it, and the output package
// renders each one.
package outputformat

import "slices"

// Format selects a built-in diagnostic representation. An empty value selects
// output from the template and oneline options.
type Format string

const (
	Text    Format = "text"
	Pretty  Format = "pretty"
	Oneline Format = "oneline"
	GCC     Format = "gcc"
	JSON    Format = "json"
	JSONL   Format = "jsonl"
	SARIF   Format = "sarif"
	GitHub  Format = "github"
)

var names = []string{
	string(Text),
	string(Pretty),
	string(Oneline),
	string(GCC),
	string(JSON),
	string(JSONL),
	string(SARIF),
	string(GitHub),
}

// Names returns the supported output formats in display order.
func Names() []string { return slices.Clone(names) }

// Valid reports whether name is a supported output format.
func Valid(name string) bool { return slices.Contains(names, name) }
