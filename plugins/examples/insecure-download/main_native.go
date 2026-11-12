//go:build !tinygo && !wasm

package main

import (
	_ "embed"
	"fmt"
	"os"

	"actionlint.kjanat.dev/v2/plugins/sdk/go/packaging"
)

//go:embed LICENSE
var licenseText string

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run . component.wasm output-directory")
		os.Exit(2)
	}
	plugin := packaging.Package{
		Identity: packaging.Identity{
			ID: "example/insecure-download", Name: "Insecure download example",
			Description: "Detect disabled TLS verification in download commands",
			License:     "MIT", Repository: "https://github.com/kjanat/actionlint", Version: "0.1.0",
		},
		Engine:    packaging.Engine{Name: "insecure-download", Version: "0.1.0"},
		Languages: []string{"bash", "sh"}, Descriptor: describe(), LicenseText: licenseText,
	}
	if err := plugin.Write(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
