// Package actionlint embeds the JSON schemas published at the module root.
//
// An embed directive cannot reach files outside its package's directory, so
// the published schemas are embedded here rather than copied under internal/.
// Internal code reads them through internal/input/configuration/configschema.
package actionlint

import "embed"

// SchemaFiles holds actionlint.schema.json and every schemas/<tool>/*.schema.json
// file, at their published slash-separated paths.
//
//go:embed actionlint.schema.json schemas/*/*.schema.json
var SchemaFiles embed.FS
