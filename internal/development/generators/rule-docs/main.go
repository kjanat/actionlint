// Command generate writes the rule reference from the shared catalogue.
package main

import (
	"context"
	"log"

	genfile "actionlint.kjanat.dev/v2/internal/development/generators/files"
)

func main() {
	if err := genfile.Write(context.Background(), "docs/rules.md", Reference(), false); err != nil {
		log.Fatal(err)
	}
}
