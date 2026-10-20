package fuzz

import (
	"testing"

	glob "actionlint.kjanat.dev/internal/semantics/glob"
)

func FuzzGlobGitRef(f *testing.F) {
	f.Add([]byte("main"))
	f.Add([]byte("releases/**"))
	f.Add([]byte("v[0-9]+.*"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		glob.ValidateRefGlob(string(data))
	})
}

func FuzzGlobFilePath(f *testing.F) {
	f.Add([]byte("**/*.go"))
	f.Add([]byte("!docs/**"))
	f.Add([]byte("src/[abc]/?.ts"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		glob.ValidatePathGlob(string(data))
	})
}
