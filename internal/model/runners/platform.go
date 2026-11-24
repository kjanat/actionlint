// Package runners resolves workflow runner declarations and platform behavior.
package runners

type Platform int

const (
	Any Platform = iota
	MacOrLinux
	Windows
)
