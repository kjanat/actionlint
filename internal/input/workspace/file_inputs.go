package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
)

type File struct {
	Path     string
	Explicit bool
}

// FileSelection discovery reads no workflow bytes and validates no repository configuration.
type FileSelection struct {
	Files  []File
	Inputs []string
}

// ExpandHome expands a leading ~/ or ~\ without interpreting shell expressions.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// FindFiles lets literal file arguments bypass Git ignore rules.
func FindFiles(ctx context.Context, paths []string, noIgnore bool) (FileSelection, error) {
	return FindFilesWithFilesystem(ctx, paths, noIgnore, files.DiskFilesystem{}, nil)
}

// FindFilesWithFilesystem runs local Git commands for ignore rules when query is nil.
func FindFilesWithFilesystem(ctx context.Context, paths []string, noIgnore bool, filesystem files.Filesystem, query discovery.GitQuery) (FileSelection, error) {
	identity := func(path string) string {
		path = absPath(path)
		if physical, err := filesystem.EvalSymlinks(path); err == nil {
			path = physical
		}
		if filesystem.Platform() == "windows" {
			path = strings.ToLower(path)
		}
		return path
	}
	var candidates []File
	explicit := make(map[string]bool)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return FileSelection{}, err
		}
		path, err := ExpandHome(path)
		if err != nil {
			return FileSelection{}, err
		}
		info, statErr := filesystem.Stat(path)
		if statErr != nil && strings.ContainsAny(path, "*?[") {
			matches, err := filesystem.Glob(path)
			if err != nil {
				return FileSelection{}, fmt.Errorf("invalid input glob %q: %w", path, err)
			}
			if len(matches) == 0 {
				return FileSelection{}, fmt.Errorf("no paths matched input glob %q", path)
			}
			for _, match := range matches {
				candidates = append(candidates, File{Path: match})
			}
			continue
		}
		literal := statErr != nil || !info.IsDir()
		candidates = append(candidates, File{Path: path, Explicit: literal})
		if literal {
			explicit[identity(path)] = true
		}
	}
	listed := &InputFiles{}
	reader := files.NewFilesystem(ctx, filesystem, listed.Add)
	seen, directories := make(map[string]bool), make(map[string]bool)
	options := discovery.Discovery{NoIgnore: noIgnore, Repositories: true, QueryGit: query, SkipDirectory: func(path string) bool {
		key := identity(path)
		if directories[key] {
			return true
		}
		directories[key] = true
		return false
	}}
	var files []File
	add := func(file File) {
		key := identity(file.Path)
		if seen[key] || !file.Explicit && explicit[key] {
			return
		}
		seen[key] = true
		files = append(files, file)
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return FileSelection{}, err
		}
		if candidate.Explicit {
			add(candidate)
			continue
		}
		path, err := filepath.Abs(candidate.Path)
		if err != nil {
			return FileSelection{}, err
		}
		info, err := filesystem.Lstat(path)
		if err != nil {
			return FileSelection{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// A named directory alias is intentional; globbed aliases are not.
			literal := false
			for _, input := range paths {
				expanded, err := ExpandHome(input)
				if err != nil {
					return FileSelection{}, err
				}
				literal = literal || absPath(expanded) == path
			}
			if literal {
				info, err = filesystem.Stat(path)
				if err != nil {
					return FileSelection{}, err
				}
			} else if target, err := filesystem.Stat(path); err != nil {
				return FileSelection{}, err
			} else if target.IsDir() {
				continue
			}
		}
		if info.IsDir() {
			found, err := discovery.Documents(ctx, path, reader, options)
			if err != nil {
				return FileSelection{}, err
			}
			for _, file := range found {
				listed.Add(file)
				add(File{Path: file})
			}
			continue
		}
		included, err := discovery.DocumentFile(ctx, path, reader, options)
		if err != nil {
			return FileSelection{}, err
		}
		if included {
			listed.Add(path)
			add(File{Path: path})
		}
	}
	if len(paths) != 0 && len(files) == 0 {
		return FileSelection{}, errors.New("no workflow or action files were found in the selected paths")
	}
	return FileSelection{Files: files, Inputs: listed.List()}, nil
}
