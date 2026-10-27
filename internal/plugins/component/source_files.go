package component

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"
	filesbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/sourcefiles"
)

const SourceFilesInterface = filesbind.InterfaceName

type (
	sourceRootsKey struct{}
	sourceFiles    struct{}
)

func (sourceFiles) Roots(ctx context.Context) ([]filesbind.SourceRoot, error) {
	roots, _ := ctx.Value(sourceRootsKey{}).([]analyzer.SourceRoot)
	result := make([]filesbind.SourceRoot, len(roots))
	for i, root := range roots {
		result[i] = filesbind.SourceRoot{Host: root.Host, Guest: root.Guest}
	}
	return result, nil
}
