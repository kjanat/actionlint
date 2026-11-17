package files

import (
	"context"
	"os"
)

// OpenShared permits a read handle to coexist with a Windows rename handle.
func OpenShared(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openSharedFile(path)
}
