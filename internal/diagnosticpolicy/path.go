package diagnosticpolicy

import (
	"path/filepath"
)

// RelativePath normalizes diagnostic paths relative to the invocation directory.
func RelativePath(directory, source string) string {
	if !filepath.IsAbs(source) {
		return source
	}
	// In-memory sources need not exist on disk. Use physical paths only when
	// both resolve; otherwise retain both original spellings.
	if resolvedDirectory, err := filepath.EvalSymlinks(directory); err == nil {
		if resolvedSource, err := filepath.EvalSymlinks(source); err == nil {
			directory, source = resolvedDirectory, resolvedSource
		}
	}
	if relative, err := filepath.Rel(directory, source); err == nil {
		return relative
	}
	return source
}
