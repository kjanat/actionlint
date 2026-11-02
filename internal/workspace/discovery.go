package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// DirectoryPaths searches workflows and actions across repository boundaries.
// Each repository supplies its own Git ignore rules.
func DirectoryPaths(ctx context.Context, dir string, reader *fsread.Reader) ([]string, error) {
	return projectpath.Documents(ctx, dir, reader, projectpath.Discovery{Repositories: true})
}

func RepositoryPaths(ctx context.Context, project *Project, reader *fsread.Reader) ([]string, error) {
	return projectpath.Documents(ctx, project.RootDir(), reader, projectpath.Discovery{})
}

func WorkflowPaths(reader *fsread.Reader, dir string) ([]string, error) {
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
