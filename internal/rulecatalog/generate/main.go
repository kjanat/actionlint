// Command generate writes the rule reference from the shared catalogue.
package main

import (
	"context"
	"log"

	"actionlint.kjanat.dev/internal/genfile"
	"actionlint.kjanat.dev/internal/rulecatalog"
)

func main() {
	if err := genfile.Write(context.Background(), "docs/rules.md", rulecatalog.Reference(), false); err != nil {
		log.Fatal(err)
	}
}
