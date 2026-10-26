// Command actionlintarch checks architecture without golangci-lint suppression filters.
package main

import (
	"actionlint.kjanat.dev/v2/analyzers/actionlintarch"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(actionlintarch.Analyzer) }
