// Package runnerpaths resolves runner paths against the local input filesystem.
package runnerpaths

import (
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

// ResolveDirectory finds an existing local directory for the runner directory.
// Case-insensitive checkout lookup records directory listings through reader.
func ResolveDirectory(paths execution.Paths, directory execution.Directory, reader *files.Reader) execution.Directory {
	unknown := execution.Directory{execution.DirectoryUnknown, paths.Analysis}
	if directory.Kind == execution.DirectoryUnknown {
		return unknown
	}
	paths = paths.DirectoryOrigin(directory)
	local, ok := AnalysisPath(paths, directory.Path, reader)
	if !ok {
		return unknown
	}
	local, err := filepath.EvalSymlinks(local)
	if err != nil {
		return unknown
	}
	info, err := os.Stat(local)
	if err != nil || !info.IsDir() {
		return unknown
	}
	return execution.Directory{execution.DirectoryKnown, local}
}

// LocalPath translates a workspace-relative runner path to the local self checkout.
func LocalPath(paths execution.Paths, relativePath string, reader *files.Reader) (string, bool) {
	relativePath, representable := execution.RunnerRelativePath(relativePath, paths.Platform)
	if !representable {
		return "", false
	}
	path, known := AnalysisPath(paths, relativePath, reader)
	if !known {
		return "", false
	}
	relative, err := filepath.Rel(paths.Workspace, path)
	return path, err == nil && filepath.IsLocal(relative)
}

// AnalysisPath resolves available files, including those outside the indexed
// repository. Case-insensitive checkouts are canonicalized through reader.
func AnalysisPath(paths execution.Paths, value string, reader *files.Reader) (string, bool) {
	relativePath, known := execution.LocalDirectoryPath(value, paths.Platform)
	if !known {
		return "", false
	}
	if filepath.IsAbs(relativePath) {
		return relativePath, true
	}
	if strings.HasPrefix(relativePath, "/") || strings.ContainsRune(relativePath, '\x00') {
		return "", false
	}
	checkout, known := paths.CheckoutFor(relativePath)
	if !known {
		return "", false
	}
	caseInsensitive := paths.CaseInsensitive
	if placement := paths.Placements.Matching(relativePath); placement.Present() {
		caseInsensitive = placement.Details().CaseInsensitive
	}
	relativePath = filepath.FromSlash(relativePath)
	if checkout != "" {
		var err error
		relativePath, err = filepath.Rel(filepath.FromSlash(checkout), filepath.Clean(relativePath))
		if err != nil {
			return "", false
		}
	}
	if caseInsensitive && filepath.IsLocal(relativePath) {
		canonical, known := caseInsensitiveRepositoryPath(paths.Workspace, filepath.ToSlash(relativePath), reader)
		if !known {
			return "", false
		}
		relativePath = filepath.FromSlash(canonical)
	}
	return filepath.Join(paths.Workspace, relativePath), true
}
