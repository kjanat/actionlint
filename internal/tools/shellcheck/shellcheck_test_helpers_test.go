package shellcheck

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

func newShellcheckRunForTest(cmd *externalCommand) *shellcheckRun {
	return &shellcheckRun{cmd: cmd, paths: runPaths{reader: fsread.New(context.Background(), nil, nil)}}
}
