package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/files"
)

// DirectoryPaths searches workflows and actions across repository boundaries.
// Each repository supplies its own Git ignore rules.
func DirectoryPaths(ctx context.Context, dir string, reader *files.Reader) ([]string, error) {
	return discovery.Documents(ctx, dir, reader, discovery.Discovery{Repositories: true})
}

func RepositoryPaths(ctx context.Context, project *Project, reader *files.Reader) ([]string, error) {
	return discovery.Documents(ctx, project.RootDir(), reader, discovery.Discovery{})
}

func WorkflowPaths(reader *files.Reader, dir string) ([]string, error) {
	paths := []string{}
	if err := reader.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not read files in %q: %w", dir, err)
	}
	slices.Sort(paths)
	return paths, nil
}
