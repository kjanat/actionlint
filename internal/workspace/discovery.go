package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// DirectoryPaths discovers workflows and action manifests within dir. Repository
// roots and .github directories use the standard workflows location; an explicitly
// selected ordinary directory is a recursive YAML input directory.
func DirectoryPaths(ctx context.Context, dir string, reader *fsread.Reader) ([]string, error) {
	workflows := dir
	switch {
	case filepath.Base(dir) == "workflows" && filepath.Base(filepath.Dir(dir)) == ".github":
		return projectpath.Workflows(ctx, dir, reader)
	case projectpath.RepositoryRoot(dir) == dir:
		workflows = filepath.Join(dir, ".github", "workflows")
	case filepath.Base(dir) == ".github":
		workflows = filepath.Join(dir, "workflows")
	}
	if workflows == dir {
		if info, err := os.Stat(filepath.Join(dir, ".github")); err == nil && info.IsDir() {
			workflows = filepath.Join(dir, ".github", "workflows")
		}
	}
	if workflows == dir {
		return projectpath.YAML(ctx, dir, reader)
	}
	return projectDocumentPaths(ctx, dir, workflows, reader)
}

func RepositoryPaths(ctx context.Context, project *Project, reader *fsread.Reader) ([]string, error) {
	return projectDocumentPaths(ctx, project.RootDir(), project.WorkflowsDir(), reader)
}

func projectDocumentPaths(ctx context.Context, dir, workflows string, reader *fsread.Reader) ([]string, error) {
	dir, workflows = filepath.Clean(dir), filepath.Clean(workflows)
	paths, err := projectpath.Actions(ctx, dir, reader)
	if err != nil {
		return nil, err
	}
	// Explicit directory aliases are supported, but discovery must not follow
	// a symlinked .github or workflows directory encountered below that input.
	for child := workflows; child != dir; child = filepath.Dir(child) {
		info, err := os.Lstat(child)
		if os.IsNotExist(err) || err == nil && info.Mode()&os.ModeSymlink != 0 {
			return paths, nil
		}
		if err != nil {
			return nil, err
		}
	}
	files, err := projectpath.Workflows(ctx, workflows, reader)
	if err != nil {
		return nil, err
	}
	paths = append(paths, files...)
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
