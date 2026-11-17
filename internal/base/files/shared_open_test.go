package files

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenSharedHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader, err := OpenShared(ctx, filepath.Join(t.TempDir(), "missing"))
	if reader != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open returned reader=%v error=%v", reader, err)
	}
}
