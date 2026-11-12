package schema

import "embed"

//go:embed assets/actionlint.schema.json assets/schemas/*/*.schema.json
var schemaFiles embed.FS

// Schema returns a JSON schema published at the module root, named by its
// slash-separated path: "actionlint.schema.json", which validates
// configuration files, or a file under schemas/, such as the tool settings
// schemas that actionlint.schema.json references.
func Schema(name string) ([]byte, error) { return schemaFiles.ReadFile("assets/" + name) }
