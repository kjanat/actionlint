// Package checkrequest describes requested workflow and action sources before checking.
package checkrequest

import (
	"io"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

type SourceKind uint8

const (
	Files SourceKind = iota
	Repository
	Stdin
	Prepared
)

type Request struct {
	Selection *workspace.FileSelection
	Sources   []workspace.InputFile
	Kind      SourceKind
	// Paths are relative to the process directory.
	Paths                []string
	Directory            string
	Reader               io.Reader
	Name                 string
	NormalizePath        bool
	AllowEmptyRepository bool
}

func FromFiles(selection workspace.FileSelection) Request {
	selection.Files = slices.Clone(selection.Files)
	selection.Inputs = slices.Clone(selection.Inputs)
	paths := make([]string, 0, len(selection.Files))
	for _, file := range selection.Files {
		paths = append(paths, file.Path)
	}
	return Request{Kind: Files, Paths: paths, Selection: &selection}
}
