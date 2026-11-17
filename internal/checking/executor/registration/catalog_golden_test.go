package registration

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/catalog.golden.json")

// catalogEntry is the observable shape of one native rule: its declaration
// and which execution hooks its registration supplies.
type catalogEntry struct {
	Descriptor      rule.Descriptor `json:"descriptor"`
	SupportsActions bool            `json:"supports_actions"`
	Scope           uint8           `json:"scope"`
	Options         string          `json:"options"`
	Execution       string          `json:"execution"`
	Prepare         bool            `json:"prepare"`
	Enabled         bool            `json:"enabled"`
}

func catalogSnapshot(t *testing.T) []catalogEntry {
	t.Helper()
	declarations := Declarations()
	if _, err := configuration.NewRuleSet(declarations); err != nil {
		t.Fatal(err)
	}
	if len(All) != len(declarations) {
		t.Fatalf("got %d native rules for %d declarations", len(All), len(declarations))
	}
	out := make([]catalogEntry, len(declarations))
	for i, declaration := range declarations {
		native := All[i]
		entry := catalogEntry{
			Descriptor: declaration.Descriptor, SupportsActions: declaration.SupportsActions,
			Scope: uint8(declaration.Scope), Execution: "none",
			Prepare: native.Prepare != nil, Enabled: native.Enabled != nil,
		}
		if declaration.Options != nil {
			entry.Options = fmt.Sprintf("%T", declaration.Options())
		}
		if _, analyzes := native.ScriptAnalyzer(); native.Visits() {
			entry.Execution = "build"
		} else if analyzes {
			entry.Execution = "script"
		}
		if native.Name != declaration.Name {
			t.Fatalf("native rule %d is %q, want %q", i, native.Name, declaration.Name)
		}
		out[i] = entry
	}
	return out
}

// TestCatalogMatchesGolden pins every native declaration and its execution
// shape so that restructuring how rules are defined cannot change them.
func TestCatalogMatchesGolden(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "catalog.golden.json")
	got, err := json.MarshalIndent(catalogSnapshot(t), "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("native catalog differs from %s; rerun with -update only for an intended change", path)
	}
}
