package actionlintarch

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestArchitecture(t *testing.T) {
	files := map[string]string{
		"source/source.go":     `package source; type Reference struct { Document string }`,
		"analyzer/analyzer.go": `package analyzer; type Runtime struct { Shell string }; type Invocation struct { Depth int }`,
		"internal/sourceid/source.go": `package sourceid // want package:"project dependency paths"
import "actionlint.kjanat.dev/v2/source"
func New() source.Reference { return source.Reference{Document:"ok"} }
`,
		"internal/tools/shellcheck/shellcheck.go": `package shellcheck // want package:"project dependency paths"
import (
 alias "actionlint.kjanat.dev/v2/analyzer"
 "actionlint.kjanat.dev/v2/source"
 ctx "context"
)
type Ref = source.Reference
func bad() {
 _ = alias.Runtime{Shell:"sh"} // want "ALA103"
 _ = Ref{Document:"bad"} // want "ALA103"
 var ref Ref
 ref.Document="bad" // want "ALA103"
 _ = ctx.Background() // want "ALA105"
 _ = ctx.TODO() // want "ALA105"
 background := ctx.Background // want "ALA105"
 _ = background()
 //nolint:actionlintarch
 _ = alias.Runtime{Shell:"bash"} // want "ALA103"
}
func good(ctx ctx.Context, ref source.Reference) source.Reference { return ref }
`,
		"internal/tools/shellcheck/shellcheck_test.go": `package shellcheck
import "actionlint.kjanat.dev/v2/source"
func fixture() { _ = source.Reference{Document:"invalid fixture permitted"} }
`,
		"internal/tools/execution/execution.go": `package execution // want package:"project dependency paths"
import "actionlint.kjanat.dev/v2/analyzer"
type Scope struct {
 Caller string // want "ALA104"
 Call int // want "ALA104"
 Depth int // want "ALA104"
 Document string // want "ALA104"
}
func runtime() analyzer.Runtime { return analyzer.Runtime{Shell:"sh"} }
`,
		"internal/engine/engine.go": `package engine
type Linter struct{} // want "ALA104"
func NewLinter() {} // want "ALA104"
`,
		"internal/config/config.go": `package config; type Config struct{}`,
		"internal/stringutil/helper.go": `package stringutil
import "actionlint.kjanat.dev/v2/internal/config"
var C config.Config
`,
		"internal/expression/expression.go": `package expression // want package:"project dependency paths"
import (
 _ "actionlint.kjanat.dev/v2/internal/stringutil" // want "ALA101.*stringutil.*config"
 _ "os" // want "ALA102"
 _ "go/ast" // want "ALA102"
 "fmt"
 "net/url"
)
var _ = fmt.Sprint
var _ = url.Parse
`,
		"internal/expression/fixture_test.go": `package expression
import "os"
func fixtureBytes() { _, _ = os.ReadFile("testdata/input") }
`,
		"internal/unclassified/new.go": `package unclassified // want "ALA100"`,
		"internal/cli/main.go": `package cli // want package:"project dependency paths"
import "context"
func entry() context.Context { return context.Background() }
`,
	}
	dir := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(dir, "src", "actionlint.kjanat.dev", "v2", name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	analysistest.Run(t, dir, Analyzer,
		module+"/internal/tools/shellcheck", module+"/internal/tools/execution", module+"/internal/engine",
		module+"/internal/expression", module+"/internal/unclassified", module+"/internal/cli", module+"/internal/sourceid")
}
