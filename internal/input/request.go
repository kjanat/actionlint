// Package input describes requested workflow and action sources before checking.
package input

import (
	"io"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

// Kind distinguishes explicit empty file sets from repository discovery.
type Kind uint8

const (
	Files Kind = iota
	Repository
	Stdin
	Prepared
)

// Request describes where sources come from. AllowEmptyRepository implements the
// pre-commit convention; it still validates and records selected configuration.
type Request struct {
	Selection *workspace.FileSelection
	Sources   []workspace.Source
	Kind      Kind
	// Paths selects files and directories relative to the process directory.
	// The check working directory controls configuration and display paths instead.
	Paths                []string
	Directory            string
	Reader               io.Reader
	Name                 string
	NormalizePath        bool
	AllowEmptyRepository bool
}

// FromFiles preserves literal-file intent after filesystem discovery, including
// when a host partitions one search across independent repository plugin sets.
func FromFiles(selection workspace.FileSelection) Request {
	selection.Files = slices.Clone(selection.Files)
	selection.Inputs = slices.Clone(selection.Inputs)
	paths := make([]string, 0, len(selection.Files))
	for _, file := range selection.Files {
		paths = append(paths, file.Path)
	}
	return Request{Kind: Files, Paths: paths, Selection: &selection}
}
