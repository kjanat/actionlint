// Package commitish contains the reference resolver shipped with actionlint.
package commitish

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"slices"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

//go:embed plugin.wasm
var component []byte

func Artifact() *artifacts.Artifact {
	digest := sha256.Sum256(component)
	return &artifacts.Artifact{
		Info: artifacts.Info{
			Source: "builtin:commitish", Kind: artifacts.Component,
			SHA256: hex.EncodeToString(digest[:]), Size: len(component), ModuleSize: len(component),
		},
		Bytes: slices.Clone(component),
	}
}
