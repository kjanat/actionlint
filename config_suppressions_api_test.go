package actionlint_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev"
)

func TestSuppressionsPolicyInspection(t *testing.T) {
	for _, tc := range []struct {
		value, report string
		set           bool
	}{
		{"null", "", false}, {"false", "", true},
		{"true", "all", true}, {"{}", "all", true},
		{"{report: suppression}", "suppression", true},
		{"{report: violation}", "violation", true},
		{"{report: all}", "all", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := actionlint.ParseConfig([]byte("policy: {disallow-suppressions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			p := cfg.Policy.DisallowSuppressions
			if (p != nil) != tc.set || p.Enabled() != (tc.report != "") || p.Report() != tc.report || p.Rules() != nil {
				t.Fatalf("set=%v enabled=%v report=%q rules=%v", p != nil, p.Enabled(), p.Report(), p.Rules())
			}
		})
	}
	for _, p := range []*actionlint.SuppressionsPolicy{nil, {}} {
		if p.Enabled() || p.Report() != "" || p.Rules() != nil {
			t.Fatal("nil and zero policies must permit inline suppressions")
		}
	}
}

func TestSuppressionsPolicyConstruction(t *testing.T) {
	for _, report := range []string{"suppression", "violation", "all"} {
		for _, rules := range [][]string{nil, {"cache-operation", "cache-operation", "cache-write-untrusted"}} {
			p, err := actionlint.DisallowSuppressions(report, rules...)
			if err != nil {
				t.Fatal(err)
			}
			yaml := "policy: {disallow-suppressions: {report: " + report
			if len(rules) != 0 {
				yaml += ", rules: [cache-operation, cache-write-untrusted]"
			}
			cfg, err := actionlint.ParseConfig([]byte(yaml + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			parsed := cfg.Policy.DisallowSuppressions
			if !p.Enabled() || p.Report() != parsed.Report() || !slices.Equal(p.Rules(), parsed.Rules()) {
				t.Fatalf("constructor and YAML differ for %s, %v", report, rules)
			}
			if len(rules) != 0 {
				rules[0] = "expression"
				returned := p.Rules()
				returned[0] = "expression"
				if !slices.Equal(p.Rules(), parsed.Rules()) {
					t.Fatal("caller-owned slice changed policy state")
				}
			}
		}
	}
	for _, tc := range []struct {
		report string
		rules  []string
	}{
		{"", nil}, {"both", nil}, {"ALL", nil},
		{"all", []string{""}}, {"all", []string{"expression"}},
		{"all", []string{"cache-operation", "typo"}},
	} {
		if p, err := actionlint.DisallowSuppressions(tc.report, tc.rules...); err == nil || p != nil {
			t.Fatalf("accepted invalid policy %q, %v", tc.report, tc.rules)
		}
	}
}
