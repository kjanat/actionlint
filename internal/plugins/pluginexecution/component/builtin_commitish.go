package component

import (
	_ "embed"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

//go:embed builtin_commitish.wasm
var builtinCommitishComponent []byte

func BuiltinCommitishArtifact() *artifacts.Artifact {
	return &artifacts.Artifact{
		Info: artifacts.ArtifactProvenance{
			Source: "builtin:commitish", Kind: artifacts.Component,
			SHA256: sha256hex.Digest(builtinCommitishComponent), Size: len(builtinCommitishComponent), ModuleSize: len(builtinCommitishComponent),
		},
		Bytes: slices.Clone(builtinCommitishComponent),
	}
}
