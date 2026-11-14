package policy

import (
	"path/filepath"
	"strings"
)

// RelativePath normalizes diagnostic paths relative to the invocation directory.
func RelativePath(directory, source string) string {
	if !filepath.IsAbs(source) {
		return source
	}
	if relative, err := filepath.Rel(directory, source); err == nil {
		return relative
	}
	return source
}

func (processor Processor) relativePath(directory, path string) string {
	canonicalDirectory, directoryKnown := processor.CanonicalPaths[directory]
	canonicalPath, pathKnown := processor.CanonicalPaths[path]
	if !pathKnown {
		if relative := RelativePath(directory, path); !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return relative
		}
	} else {
		path = canonicalPath
	}
	if directoryKnown {
		directory = canonicalDirectory
	}
	return RelativePath(directory, path)
}

func (processor Processor) displayPath(directory, path string) string {
	relative := RelativePath(directory, path)
	if _, supplied := processor.CanonicalPaths[path]; supplied || !filepath.IsAbs(path) || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	if canonicalDirectory, known := processor.CanonicalPaths[directory]; known {
		return RelativePath(canonicalDirectory, path)
	}
	return relative
}

func (processor Processor) projectRelativePath(root, path string) string {
	if !filepath.IsAbs(path) || root == "" {
		return path
	}
	// An alias inside the project has its own configured file scope.
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	return processor.relativePath(root, path)
}
