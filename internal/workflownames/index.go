// Package workflownames indexes repository workflow names once per analysis.
package workflownames

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// Names is a complete inventory only when every workflow name could be read.
type Names struct {
	Values   map[string]bool
	Complete bool
}

// Index shares immutable inventories across concurrent workflow checks.
type Index struct {
	Load func(string) (name string, known bool, err error)
	// OnDirectory records directory discovery as an incremental dependency.
	OnDirectory func(string)
	// Paths includes in-memory source files which may not exist on disk yet.
	Paths []string
	mu    sync.Mutex
	cache map[string]func() (Names, error)
}

// SamePath compares checkout aliases while preserving distinct workflow filenames.
func SamePath(left, right string) bool {
	return samePath(left, right, runtime.GOOS == "windows")
}

func samePath(left, right string, windows bool) bool {
	return samePathOnFilesystem(left, right, windows, os.Stat, os.ReadDir)
}

func samePathOnFilesystem(left, right string, windows bool, stat func(string) (os.FileInfo, error), readDir func(string) ([]os.DirEntry, error)) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if left == right {
		return true
	}
	leftInfo, leftErr := stat(left)
	rightInfo, rightErr := stat(right)
	if leftErr == nil && rightErr == nil && leftInfo.IsDir() && rightInfo.IsDir() {
		return os.SameFile(leftInfo, rightInfo)
	}
	if errors.Is(leftErr, os.ErrNotExist) && errors.Is(rightErr, os.ErrNotExist) {
		leftParent, leftSuffix := existingPathAncestor(left, stat)
		rightParent, rightSuffix := existingPathAncestor(right, stat)
		if leftParent != nil && rightParent != nil && os.SameFile(leftParent, rightParent) &&
			(leftSuffix == rightSuffix || windows && strings.EqualFold(leftSuffix, rightSuffix)) {
			return true
		}
	}
	if !strings.EqualFold(left, right) {
		if !strings.EqualFold(filepath.Base(left), filepath.Base(right)) {
			return false
		}
		leftDir, leftDirErr := stat(filepath.Dir(left))
		rightDir, rightDirErr := stat(filepath.Dir(right))
		if leftDirErr != nil || rightDirErr != nil || !leftDir.IsDir() || !rightDir.IsDir() || !os.SameFile(leftDir, rightDir) {
			return false
		}
		return samePathOnFilesystem(left, filepath.Join(filepath.Dir(left), filepath.Base(right)), windows, stat, readDir)
	}
	if leftErr == nil && rightErr == nil {
		if !os.SameFile(leftInfo, rightInfo) {
			return false
		}
		leftName, rightName := filepath.Base(left), filepath.Base(right)
		if leftName == rightName {
			return true
		}
		entries, err := readDir(filepath.Dir(left))
		if err != nil {
			return false
		}
		leftExists, rightExists := false, false
		for _, entry := range entries {
			leftExists = leftExists || entry.Name() == leftName
			rightExists = rightExists || entry.Name() == rightName
		}
		return !leftExists || !rightExists
	}
	return windows
}

func existingPathAncestor(path string, stat func(string) (os.FileInfo, error)) (os.FileInfo, string) {
	suffix := filepath.Base(path)
	for parent := filepath.Dir(path); parent != path; parent = filepath.Dir(path) {
		info, err := stat(parent)
		if err == nil {
			if info.IsDir() {
				return info, suffix
			}
			return nil, ""
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, ""
		}
		suffix = filepath.Join(filepath.Base(parent), suffix)
		path = parent
	}
	return nil, ""
}

func workflowPaths(dir string, candidates []string, windows bool) []string {
	return workflowPathsByIdentity(dir, candidates, func(left, right string) bool { return samePath(left, right, windows) })
}

func workflowPathsByIdentity(dir string, candidates []string, same func(string, string) bool) []string {
	var paths []string
	buckets := map[string][]int{}
	directories := map[string]bool{}
	for _, path := range candidates {
		if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
			continue
		}
		parent := filepath.Dir(path)
		member, cached := directories[parent]
		if !cached {
			member = same(parent, dir)
			directories[parent] = member
		}
		if !member {
			continue
		}
		key := filenameKey(path)
		index := -1
		for _, candidate := range buckets[key] {
			if same(paths[candidate], path) {
				index = candidate
				break
			}
		}
		if index >= 0 {
			paths[index] = path
		} else {
			buckets[key] = append(buckets[key], len(paths))
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// filenameKey groups only names that can compare equal under strings.EqualFold.
func filenameKey(path string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, filepath.Base(path))
}

// ForRoot includes only workflow files directly under .github/workflows.
// Input-selection filters must not remove potential workflow_run producers.
func (i *Index) ForRoot(root string) (Names, error) {
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[string]func() (Names, error){}
	}
	key := filepath.Clean(root)
	load, exists := i.cache[key]
	if !exists {
		for cached, entry := range i.cache {
			if SamePath(cached, root) {
				load, exists = entry, true
				break
			}
		}
	}
	if !exists {
		load = sync.OnceValues(func() (Names, error) {
			names := Names{Values: map[string]bool{}, Complete: true}
			dir := filepath.Join(root, ".github", "workflows")
			if i.OnDirectory != nil {
				i.OnDirectory(dir)
			}
			entries, err := os.ReadDir(dir)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				names.Complete = false
			}
			var candidates []string
			for _, entry := range entries {
				if !entry.IsDir() {
					path := filepath.Join(dir, entry.Name())
					candidates = append(candidates, path)
				}
			}
			diskPaths := map[string][]string{}
			for _, path := range candidates {
				key := filenameKey(path)
				diskPaths[key] = append(diskPaths[key], path)
			}
			candidates = append(candidates, i.Paths...)
			for _, path := range workflowPaths(dir, candidates, runtime.GOOS == "windows") {
				name, known, err := i.Load(path)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return names, err
					}
					names.Complete = false
					continue
				}
				if !known {
					names.Complete = false
					continue
				}
				if name == "" {
					filename := filepath.Base(path)
					for _, disk := range diskPaths[filenameKey(path)] {
						if SamePath(disk, path) {
							filename = filepath.Base(disk)
							break
						}
					}
					name = ".github/workflows/" + filename
				}
				names.Values[name] = true
			}
			return names, nil
		})
		i.cache[key] = load
	}
	i.mu.Unlock()
	return load()
}
