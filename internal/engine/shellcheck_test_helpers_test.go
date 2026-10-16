package engine

import (
	"context"

	"actionlint.kjanat.dev/internal/fsread"
)

func newShellcheckRunForTest(cmd *externalCommand) *shellcheckRun {
	return &shellcheckRun{cmd: cmd, paths: runPaths{reader: fsread.New(context.Background(), nil, nil)}}
}
