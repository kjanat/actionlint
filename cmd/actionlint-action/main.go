package main

import (
	"os"

	_ "time/tzdata"
)

func main() {
	a := &action{
		args:    os.Args,
		stdout:  os.Stdout,
		env:     os.Getenv,
		lint:    runLinter,
		newID:   newDelimiter,
		timeout: lintTimeout,
	}
	os.Exit(a.run())
}
