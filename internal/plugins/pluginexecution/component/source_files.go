package component

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourcefiles"
)

const SourceFilesInterface = sourcefiles.InterfaceName

type (
	sourceRootsKey struct{}
	sourceFiles    struct{}
)

func (sourceFiles) Roots(ctx context.Context) ([]sourcefiles.SourceRoot, error) {
	roots, _ := ctx.Value(sourceRootsKey{}).([]checkinputs.SourceRoot)
	result := make([]sourcefiles.SourceRoot, len(roots))
	for i, root := range roots {
		result[i] = sourcefiles.SourceRoot{Host: root.Host, Guest: root.Guest}
	}
	return result, nil
}
