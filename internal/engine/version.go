package engine

import (
	"actionlint.kjanat.dev/internal/buildinfo"
)

func InstalledFrom() string { return buildinfo.InstalledFrom() }
func Version() string       { return buildinfo.Version() }
