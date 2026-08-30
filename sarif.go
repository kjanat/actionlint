package actionlint

import _ "embed"

//go:embed sarif_template.txt
var sarifTemplate string

// SARIFTemplate returns the canonical Go template for SARIF output.
func SARIFTemplate() string {
	return sarifTemplate
}
