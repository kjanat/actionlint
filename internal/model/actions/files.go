package action

import (
	"maps"
	"slices"
	"strings"
)

type FileAvailability uint8

const (
	FileUnknown FileAvailability = iota
	FilePresent
	FileMissing
)

// ReferencedFiles records observed availability. An absent entry is unknown.
type ReferencedFiles struct{ availability map[string]FileAvailability }

func NewReferencedFiles(availability map[string]FileAvailability) ReferencedFiles {
	return ReferencedFiles{availability: maps.Clone(availability)}
}

func (f ReferencedFiles) Lookup(path string) FileAvailability { return f.availability[path] }

// FileReferences lists manifest-relative paths that can be checked on the host.
// Container entrypoints refer to files inside an image and are excluded.
func (r Runs) FileReferences() []string {
	var files []string
	using := strings.ToLower(r.Using)
	if strings.HasPrefix(using, "node") {
		files = append(files, r.Main, r.Pre, r.Post)
	}
	if using == "docker" && !ImageOnRegistry(r.Image) {
		files = append(files, r.Image)
	}
	return slices.DeleteFunc(files, func(file string) bool { return file == "" })
}

// ImageOnRegistry distinguishes accepted registry image forms from local paths.
func ImageOnRegistry(image string) bool {
	return strings.HasPrefix(image, "docker://") || strings.HasPrefix(image, "gcr.io/") || strings.HasPrefix(image, "pkg.dev/") || strings.HasPrefix(image, "ghcr.io/") || strings.HasPrefix(image, "docker.io/")
}
