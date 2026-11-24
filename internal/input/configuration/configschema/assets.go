package configschema

import actionlint "actionlint.kjanat.dev/v2"

// Schema returns a JSON schema published at the module root. The name is a
// slash-separated path.
func Schema(name string) ([]byte, error) { return actionlint.SchemaFiles.ReadFile(name) }
