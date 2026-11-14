package policy_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestPolicyUsesPreparedPathsWithoutFilesystemSources(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	checkout := filepath.Join(directory, "checkout-alias")
	physical := filepath.Join(directory, "captured-checkout")
	path := filepath.Join(directory, "source-alias.yml")
	processor := policy.Processor{
		RuleSet:        checking.ConfigurationRules(),
		CanonicalPaths: map[string]string{checkout: physical, path: filepath.Join(physical, "ci.yml")},
	}
	for _, ignore := range []bool{false, true} {
		configuration := "{}"
		if ignore {
			configuration = "ignores: [{rule: expression-type, file: ci.yml}]"
		}
		config, err := checking.ConfigurationRules().Parse([]byte(configuration))
		if err != nil {
			t.Fatal(err)
		}
		got, err := processor.Apply(policy.Policy{Path: path, ConfigPath: path, Root: checkout, ProjectRoot: checkout, WorkingDir: checkout, Config: config},
			[]*finding.Finding{{Rule: "expression-type", Path: path, Start: sourcepos.Position{Line: 1}}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ignore {
			if len(got) != 0 {
				t.Fatalf("ignore did not use the captured project path: %+v", got)
			}
		} else if len(got) != 1 || got[0].Path != filepath.Join("..", "source-alias.yml") {
			t.Fatalf("display did not retain the supplied source path: %+v", got)
		}
	}
}
