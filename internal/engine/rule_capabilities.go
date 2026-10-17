package engine

import (
	"io"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

type configuredRule interface {
	SetConfig(*repoconfig.Config)
	Config() *repoconfig.Config
}

type (
	debugRule     interface{ EnableDebug(io.Writer) }
	describedRule interface{ Description() string }
)
