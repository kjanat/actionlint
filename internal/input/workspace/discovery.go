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

// FindRepositoryFiles discovers workflow and action paths before plugins are loaded.
func FindRepositoryFiles(ctx context.Context, directory string, noIgnore bool) (FileSelection, error) {
	root := discovery.RepositoryRoot(directory)
	if root == "" {
		return FileSelection{}, nil
	}
	observed := &Inputs{}
	paths, err := discovery.Documents(ctx, root, files.New(ctx, nil, observed.Add), discovery.Discovery{NoIgnore: noIgnore})
	if err != nil {
		return FileSelection{}, err
	}
	selected := FileSelection{Files: make([]File, 0, len(paths))}
	for _, path := range paths {
		selected.Files = append(selected.Files, File{Path: path})
		observed.Add(path)
	}
	selected.Inputs = observed.List()
	return selected, nil
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
