// Package actionlint embeds the JSON schemas published at the module root.
//
// An embed directive cannot reach files outside its package's directory.
package actionlint

import "embed"

//go:embed actionlint.schema.json schemas/*/*.schema.json
var SchemaFiles embed.FS
