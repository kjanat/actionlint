package component

import (
	"context"
	"fmt"
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"github.com/kjanat/wacogo/wasi/filesystem/preopens"
)

type fileSystemsKey struct{}

type scopedPreopens struct{ deps preopens.Deps }

func (p scopedPreopens) GetDirectories(ctx context.Context) ([]preopens.TupleDescriptorString, error) {
	files, _ := ctx.Value(fileSystemsKey{}).([]analyzer.FileSystem)
	entries := make([]*preopens.PreopenEntry, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, mount := range files {
		if mount.FS == nil || !strings.HasPrefix(mount.Path, "/") || path.Clean(mount.Path) != mount.Path || seen[mount.Path] {
			return nil, fmt.Errorf("invalid plugin filesystem mount %q", mount.Path)
		}
		seen[mount.Path] = true
		entries = append(entries, &preopens.PreopenEntry{Path: mount.Path, Root: ".", FS: mount.FS})
	}
	return preopens.NewMultiFSPreopens(entries)(p.deps).GetDirectories(ctx)
}
