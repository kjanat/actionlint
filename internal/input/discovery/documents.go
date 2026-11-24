package discovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/files"
)

type Discovery struct {
	QueryGit GitQuery
	NoIgnore bool
	// Repositories enters nested repositories, applying their own Git rules.
	Repositories  bool
	SkipDirectory func(string) bool
}

func IsWorkflow(path string) bool {
	if !isYAML(filepath.Base(path)) {
		return false
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "workflows" && filepath.Base(filepath.Dir(dir)) == ".github" {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

func DocumentFile(ctx context.Context, path string, reader *files.Reader, options Discovery) (bool, error) {
	if !isManifest(filepath.Base(path)) && !IsWorkflow(path) {
		return false, nil
	}
	info, err := reader.Filesystem().Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		if info.Mode()&os.ModeSymlink == 0 || !IsWorkflow(path) {
			return false, nil
		}
		target, err := reader.Filesystem().Stat(path)
		if err != nil {
			return false, err
		}
		if !target.Mode().IsRegular() {
			return false, nil
		}
	}
	directory, err := reader.Filesystem().EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	filter, err := documentGitFilter(ctx, directory, reader, options)
	if err != nil {
		return false, err
	}
	if filter == nil {
		return true, nil
	}
	return filter.manifests[filter.key(filepath.Base(path))], nil
}

func documentGitFilter(ctx context.Context, path string, reader *files.Reader, options Discovery) (*gitFilter, error) {
	if options.NoIgnore {
		return nil, nil
	}
	if repositoryRoot(path, reader.Filesystem()) == "" {
		if options.QueryGit == nil {
			return nil, nil
		}
		// A scoped host may not expose the repository's .git directory.
		_, err := options.QueryGit(ctx, path, "rev-parse", "--show-toplevel")
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("could not locate the Git work tree for %q: %w", path, err)
		}
	}
	return gitFilterWithQuery(ctx, path, reader, options.QueryGit)
}

// Documents skips Git metadata and directory symlinks.
func Documents(ctx context.Context, root string, reader *files.Reader, options Discovery) ([]string, error) {
	physical, err := reader.Filesystem().EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	type scope struct {
		root   string
		filter *gitFilter
	}
	newScope := func(path string) (scope, error) {
		s := scope{root: path}
		var err error
		s.filter, err = documentGitFilter(ctx, path, reader, options)
		if err != nil {
			return s, fmt.Errorf("could not apply Git ignore rules in %q: %w", path, err)
		}
		return s, nil
	}
	initial, err := newScope(physical)
	if err != nil {
		return nil, err
	}
	scopes := []scope{initial}
	logical := reader.Mapped(func(path string) string {
		relative, err := filepath.Rel(physical, path)
		if err != nil {
			return path
		}
		return filepath.Join(root, relative)
	})
	var paths []string
	err = logical.WalkDir(physical, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for len(scopes) > 1 && !within(scopes[len(scopes)-1].root, path) {
			scopes = scopes[:len(scopes)-1]
		}
		current := scopes[len(scopes)-1]
		relative, err := filepath.Rel(current.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if current.filter != nil {
			key = current.filter.key(key)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			address, err := filepath.Rel(physical, path)
			if err != nil {
				return err
			}
			if options.SkipDirectory != nil && options.SkipDirectory(filepath.Join(root, address)) {
				return filepath.SkipDir
			}
			if path == physical {
				return nil
			}
			if current.filter != nil && current.filter.ignored[key] {
				return filepath.SkipDir
			}
			if _, err := reader.Filesystem().Stat(filepath.Join(path, ".git")); err == nil {
				if !options.Repositories {
					return filepath.SkipDir
				}
				nested, err := newScope(path)
				if err != nil {
					return err
				}
				scopes = append(scopes, nested)
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		if entry.Name() == ".gitignore" && current.filter != nil {
			logical.Record(path)
		}
		relative, err = filepath.Rel(physical, path)
		if err != nil {
			return err
		}
		address := filepath.Join(root, relative)
		workflow := IsWorkflow(address) || IsWorkflow(path)
		if !isManifest(entry.Name()) && !workflow || current.filter != nil && !current.filter.manifests[key] {
			return nil
		}
		if !entry.Type().IsRegular() {
			if !workflow || entry.Type()&os.ModeSymlink == 0 {
				return nil
			}
			target, err := reader.Filesystem().Stat(path)
			if err != nil {
				return err
			}
			if !target.Mode().IsRegular() {
				return nil
			}
		}
		paths = append(paths, address)
		return nil
	})
	return paths, err
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
