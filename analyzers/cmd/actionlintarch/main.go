// Command actionlintarch checks architecture without golangci-lint suppression filters.
package main

import (
	"fmt"
	"os"

	"actionlint.kjanat.dev/v2/analyzers/actionlintarch"
	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	contract, err := architecture.Load("architecture.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if contract.Module() != "actionlint.kjanat.dev/v2" {
		fmt.Fprintln(os.Stderr, "actionlintarch contract has wrong module:", contract.Module())
		os.Exit(1)
	}
	singlechecker.Main(actionlintarch.New(contract))
}
