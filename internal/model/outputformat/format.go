// Package outputformat names the built-in diagnostic representations.
package outputformat

import "slices"

// Format is empty when the template and oneline options select the output.
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

func Names() []string { return slices.Clone(names) }

func Valid(name string) bool { return slices.Contains(names, name) }
