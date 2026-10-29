package newcheck // want package:"project dependency paths"
import (
	_ "actionlint.kjanat.dev/v2/internal/config"                      // want "ALA101.*semantics.*configuration"
	_ "actionlint.kjanat.dev/v2/internal/expression/context/generate" // want "ALA101.*semantics.*tooling" "ALA102.*os"
	_ "actionlint.kjanat.dev/v2/internal/testutil"                    // want "ALA101.*semantics.*tests" "ALA101.*testutil.*config"
	_ "actionlint.kjanat.dev/v2/internal/undecided"                   // want "ALA101.*unclassified" "ALA101.*undecided.*config"
	_ "actionlint.kjanat.dev/v2/source"
)
