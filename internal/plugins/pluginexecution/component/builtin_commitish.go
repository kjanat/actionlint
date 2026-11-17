package component

import (
	_ "embed"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/sha256hex"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// builtinCommitishComponent is the reference resolver shipped with actionlint.
// builtin_commitish.source.json records the build that produced it.
//
//go:embed builtin_commitish.wasm
var builtinCommitishComponent []byte

// BuiltinCommitishArtifact returns a fresh copy of the bundled reference resolver.
func BuiltinCommitishArtifact() *artifacts.Artifact {
	return &artifacts.Artifact{
		Info: artifacts.ArtifactProvenance{
			Source: "builtin:commitish", Kind: artifacts.Component,
			SHA256: sha256hex.Digest(builtinCommitishComponent), Size: len(builtinCommitishComponent), ModuleSize: len(builtinCommitishComponent),
		},
		Bytes: slices.Clone(builtinCommitishComponent),
	}
}
