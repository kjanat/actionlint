package engine

import (
	"actionlint.kjanat.dev/internal/fsread"
	"context"
)

func newShellcheckRunForTest(cmd *externalCommand) *shellcheckRun {
	return &shellcheckRun{cmd: cmd, paths: runPaths{reader: fsread.New(context.Background(), nil, nil)}}
}
