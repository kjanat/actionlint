package policy_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestReportedPhysicalPathIsRelativeToAliasedCheckout(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	checkout := filepath.Join(directory, "checkout-alias")
	physical := filepath.Join(directory, "captured-checkout")
	processor := policy.Processor{RuleSet: checking.ConfigurationRules(), CanonicalPaths: map[string]string{checkout: physical}}
	config, err := checking.ConfigurationRules().ParseConfig([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	findings := []*finding.Finding{{Rule: "expression-type", Path: filepath.Join(physical, "scripts", "lib.sh"), Start: sourcetext.Position{Line: 1}}}
	p := preparePolicy(processor.RuleSet, config, policy.DocumentInputs{Path: "ci.yml", Root: checkout, WorkingDir: checkout}, nil, findings)
	got, err := publishFindings(processor, p, findings, nil)
	if err != nil || len(got) != 1 || got[0].Path != filepath.Join("scripts", "lib.sh") {
		t.Fatalf("reported path=%+v error=%v, want scripts/lib.sh", got, err)
	}
}

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
		config, err := checking.ConfigurationRules().ParseConfig([]byte(configuration))
		if err != nil {
			t.Fatal(err)
		}
		findings := []*finding.Finding{{Rule: "expression-type", Path: path, Start: sourcetext.Position{Line: 1}}}
		p := preparePolicy(processor.RuleSet, config, policy.DocumentInputs{Path: path, ConfigPath: path, Root: checkout, ProjectRoot: checkout, WorkingDir: checkout}, nil, findings)
		got, err := publishFindings(processor, p, findings, nil)
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

func TestReportedPathPrefersShorterSpellingOutsideWorkingDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	working := filepath.Join(directory, "work", strings.Repeat("x/", len(directory)/len("../")+1))
	processor := policy.Processor{RuleSet: checking.ConfigurationRules()}
	config, err := checking.ConfigurationRules().ParseConfig([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	far := filepath.Join(directory, "far.yml")
	sibling := filepath.Join(filepath.Dir(working), "other.yml")
	for path, want := range map[string]string{far: far, sibling: filepath.Join("..", "other.yml")} {
		findings := []*finding.Finding{{Rule: "expression-type", Path: path, Start: sourcetext.Position{Line: 1}}}
		p := preparePolicy(processor.RuleSet, config, policy.DocumentInputs{Path: "ci.yml", Root: working, WorkingDir: working}, nil, findings)
		got, err := publishFindings(processor, p, findings, nil)
		if err != nil || len(got) != 1 || got[0].Path != want {
			t.Fatalf("reported path=%+v error=%v, want %q", got, err, want)
		}
	}
}
