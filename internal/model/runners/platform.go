// Package runner resolves workflow runner declarations and platform behavior.
package runner

// Platform identifies runner path and shell conventions known to analysis.
type Platform int

const (
	Any Platform = iota
	MacOrLinux
	Windows
)
