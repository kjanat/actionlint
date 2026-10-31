package main

import (
	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(architecture.Analyzer) }
