package component

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"slices"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// builtinCommitishComponent is the reference resolver shipped with actionlint.
// builtin_commitish.source.json records the build that produced it.
//
//go:embed builtin_commitish.wasm
var builtinCommitishComponent []byte

// BuiltinCommitishArtifact returns a fresh copy of the bundled reference resolver.
func BuiltinCommitishArtifact() *artifacts.Artifact {
	digest := sha256.Sum256(builtinCommitishComponent)
	return &artifacts.Artifact{
		Info: artifacts.ArtifactProvenance{
			Source: "builtin:commitish", Kind: artifacts.Component,
			SHA256: hex.EncodeToString(digest[:]), Size: len(builtinCommitishComponent), ModuleSize: len(builtinCommitishComponent),
		},
		Bytes: slices.Clone(builtinCommitishComponent),
	}
}
