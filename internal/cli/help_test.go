package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestManualDocumentsEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("man", "actionlint.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n# FLAGS\n")
	if !ok {
		t.Fatal("manual has no FLAGS section")
	}
	body, _, _ = strings.Cut(body, "\n# ")
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`\*\*(--?[^*]+)\*\*`).FindAllStringSubmatch(body, -1) {
		documented[m[1]] = true
	}
	app := newCommandApp(t.Context(), &Command{})
	app.root.Flags().VisitAll(func(f *pflag.Flag) {
		for _, name := range []string{"--" + f.Name, "-" + f.Shorthand} {
			if name == "-" {
				continue
			}
			if !documented[name] {
				t.Errorf("manual is missing %s", name)
			}
			delete(documented, name)
		}
	})
	// Flags specific to subcommands may also be documented in FLAGS. They
	// are not part of the root's mandatory set, but are recognized commands.
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			delete(documented, "--"+f.Name)
			delete(documented, "-"+f.Shorthand)
		})
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(app.root)
	for name := range documented {
		t.Errorf("manual documents unknown flag %s", name)
	}
}
