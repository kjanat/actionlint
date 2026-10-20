// Command generate writes the rule reference from the shared catalogue.
package main

import (
	"context"
	"log"

	"actionlint.kjanat.dev/v2/internal/genfile"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
)

func main() {
	if err := genfile.Write(context.Background(), "docs/rules.md", rulecatalog.Reference(), false); err != nil {
		log.Fatal(err)
	}
}
