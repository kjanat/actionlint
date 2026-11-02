package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// File records whether a path was literally named or found by discovery.
// Only a literal file bypasses the input configuration's file filters.
type File struct {
	Path     string
	Explicit bool
}

// FileSelection contains selected paths and all observed discovery inputs.
// Discovery does not read workflow bytes or validate repository configuration.
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

// fileIdentity deduplicates physical files, retaining the selected display path.
func fileIdentity(path string) string {
	path = absPath(path)
	if physical, err := filepath.EvalSymlinks(path); err == nil {
		path = physical
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// FindFiles expands directory arguments and shell-style *, ? and [] globs.
// Glob matches remain discovery inputs; only literally named files override
// ignores. Each directory and physical file is visited at most once.
func FindFiles(ctx context.Context, paths []string, noIgnore bool) (FileSelection, error) {
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
		info, statErr := os.Stat(path)
		if statErr != nil && strings.ContainsAny(path, "*?[") {
			matches, err := filepath.Glob(path)
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
			explicit[fileIdentity(path)] = true
		}
	}
	listed := &Inputs{}
	reader := fsread.New(ctx, nil, listed.Add)
	seen, directories := make(map[string]bool), make(map[string]bool)
	options := projectpath.Discovery{NoIgnore: noIgnore, Repositories: true, SkipDirectory: func(path string) bool {
		key := fileIdentity(path)
		if directories[key] {
			return true
		}
		directories[key] = true
		return false
	}}
	var files []File
	add := func(file File) {
		key := fileIdentity(file.Path)
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
		info, err := os.Lstat(path)
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
				info, err = os.Stat(path)
				if err != nil {
					return FileSelection{}, err
				}
			} else if target, err := os.Stat(path); err != nil {
				return FileSelection{}, err
			} else if target.IsDir() {
				continue
			}
		}
		if info.IsDir() {
			found, err := projectpath.Documents(ctx, path, reader, options)
			if err != nil {
				return FileSelection{}, err
			}
			for _, file := range found {
				listed.Add(file)
				add(File{Path: file})
			}
			continue
		}
		included, err := projectpath.DocumentFile(ctx, path, reader, options)
		if err != nil {
			return FileSelection{}, err
		}
		if included {
			listed.Add(path)
			add(File{Path: path})
		}
	}
	if len(paths) != 0 && len(files) == 0 {
		return FileSelection{}, fmt.Errorf("no workflow or action files were found in the selected paths")
	}
	return FileSelection{Files: files, Inputs: listed.List()}, nil
}
