package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/projectpath"
)

func RepositoryPaths(ctx context.Context, project *Project, reader *fsread.Reader) ([]string, error) {
	paths, err := projectpath.Actions(ctx, project.RootDir(), reader)
	if err != nil {
		return nil, err
	}
	workflows := project.WorkflowsDir()
	if _, err := os.Stat(workflows); err == nil {
		files, err := WorkflowPaths(reader, workflows)
		if err != nil {
			return nil, err
		}
		paths = append(paths, files...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
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
