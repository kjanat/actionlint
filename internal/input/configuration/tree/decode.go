package tree

import "actionlint.kjanat.dev/v2/internal/input/configuration/document"

// Decode returns configuration values and their source locations.
func Decode(path string, data []byte) (document.Document, error) { return document.Parse(path, data) }
