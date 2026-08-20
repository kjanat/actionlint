package actionlint_fuzz

import (
	"testing"

	"github.com/kjanat/actionlint"
)

func FuzzGlobGitRef(f *testing.F) {
	f.Add([]byte("main"))
	f.Add([]byte("releases/**"))
	f.Add([]byte("v[0-9]+.*"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		actionlint.ValidateRefGlob(string(data))
	})
}

func FuzzGlobFilePath(f *testing.F) {
	f.Add([]byte("**/*.go"))
	f.Add([]byte("!docs/**"))
	f.Add([]byte("src/[abc]/?.ts"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		actionlint.ValidatePathGlob(string(data))
	})
}
