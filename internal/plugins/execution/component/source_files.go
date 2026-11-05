package component

import (
	"context"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	filesbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourcefiles"
)

const SourceFilesInterface = filesbind.InterfaceName

type (
	sourceRootsKey struct{}
	sourceFiles    struct{}
)

func (sourceFiles) Roots(ctx context.Context) ([]filesbind.SourceRoot, error) {
	roots, _ := ctx.Value(sourceRootsKey{}).([]checkinputs.SourceRoot)
	result := make([]filesbind.SourceRoot, len(roots))
	for i, root := range roots {
		result[i] = filesbind.SourceRoot{Host: root.Host, Guest: root.Guest}
	}
	return result, nil
}
