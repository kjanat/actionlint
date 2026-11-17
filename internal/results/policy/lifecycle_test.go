package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

const lifecycleConfig = `lint:
  lifecycle:
    policies:
      - id: migrate
        description: Prepare for retirement
        match: {subjects: [runners], events: [support-ends]}
        notifications:
          - {starts: {before: 180d}, severity: info}
          - {starts: {before: 90d}, severity: warning}
          - {starts: {on: effective-date}, severity: error}
`

func lifecycleDate(t *testing.T, s string) lifecycle.Date {
	t.Helper()
	d, err := lifecycle.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func lifecycleFinding(t *testing.T, asOf string, current bool) *finding.Finding {
	t.Helper()
	svc, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	return &finding.Finding{Rule: "runner-support", Code: "unsupported", Severity: "warning", Message: "retirement", Start: sourcetext.Position{Line: 2, Column: 1}, LifecycleEvidence: &lifecycle.Evidence{Subject: lifecycle.Runners, Name: "runner", Kind: lifecycle.SupportEnds, Service: svc, Effective: lifecycleDate(t, "2026-12-01"), AsOf: lifecycleDate(t, asOf), Current: current}}
}

func lifecycleRulesConfig(t *testing.T, text string) *configuration.Config {
	t.Helper()
	rules := checking.ConfigurationRules()
	cfg, err := rules.ParseConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = rules.ResolveRules(configuration.Presets{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func lifecycleApply(t *testing.T, cfg *configuration.Config, f *finding.Finding, edit func(*policy.DocumentInputs)) []*finding.Finding {
	t.Helper()
	p := policy.DocumentInputs{Path: "workflow.yml", Content: []byte("# actionlint-ignore-next-line runner-support -- intentional\nruns-on: old\n")}
	if edit != nil {
		edit(&p)
	}
	rules := checking.ConfigurationRules()
	var sources map[string]*configuration.Config
	if f.Path != "" && f.Path != p.Path {
		sources = map[string]*configuration.Config{f.Path: cfg}
	}
	input := []*finding.Finding{f}
	p = preparePolicy(rules, cfg, p, sources, input)
	got, err := publishFindings(policy.Processor{RuleSet: rules}, p, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func lifecycleRequired(t *testing.T) []lifecycle.Policy {
	t.Helper()
	p, err := lifecycle.NewPolicy("required", "Required migration", lifecycle.Match{Subjects: []lifecycle.Subject{lifecycle.Runners}, Events: []lifecycle.EventKind{lifecycle.SupportEnds}}, []lifecycle.Notification{{Starts: lifecycle.Before(180), Severity: severity.Warning}}, "trusted.yml")
	if err != nil {
		t.Fatal(err)
	}
	return []lifecycle.Policy{p}
}

func TestLifecycleOptionalWindowsAndRuleLevels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date, extra, want string
		current, withPolicy     bool
	}{
		{"existing finding without policy", "2026-09-01", "", "warning", true, false},
		{"future finding without policy", "2026-09-01", "", "", false, false},
		{"outside window", "2026-04-01", "", "", true, true},
		{"information", "2026-09-01", "", "info", true, true},
		{"warning", "2026-09-15", "", "warning", true, true},
		{"deadline", "2026-12-01", "", "error", true, true},
		{"explicit warning", "2026-09-01", "  rules: {correctness: {runner-support: warn}}\n", "warning", true, true},
		{"explicit error", "2026-09-01", "  rules: {correctness: {runner-support: error}}\n", "error", true, true},
		{"explicit on", "2026-09-01", "  rules: {correctness: {runner-support: on}}\n", "info", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := "{}"
			if tc.withPolicy {
				text = lifecycleConfig
				if tc.extra != "" {
					text += tc.extra
				}
			}
			cfg := lifecycleRulesConfig(t, text)
			got := lifecycleApply(t, cfg, lifecycleFinding(t, tc.date, tc.current), func(p *policy.DocumentInputs) { p.Content = []byte("on: push\nruns-on: old\n") })
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v", got)
				}
				return
			}
			if len(got) != 1 || string(got[0].Severity) != tc.want {
				t.Fatalf("got %+v; want %s", got, tc.want)
			}
		})
	}
}

func TestLifecycleIntroductionPolicyPreservesUnavailableRuntime(t *testing.T) {
	t.Parallel()
	const policyConfig = `lint:
  lifecycle:
    policies:
      - id: introduction
        description: Notify when a language runtime becomes available
        match: {subjects: [language-runtimes], events: [support-starts]}
        notifications:
          - {starts: {on: effective-date}, severity: warning}
`
	for _, tc := range []struct {
		name, config string
		want         int
	}{
		{"default", "{}", 1},
		{"notification not started", policyConfig, 1},
		{"rule disabled", policyConfig + "  rules: {correctness: {runtime-lifecycle: off}}\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := lifecycleFinding(t, "2026-01-01", true)
			evidence, message, known := lifecycle.NodeLifecycle("26", lifecycleDate(t, "2026-01-01"), f.LifecycleEvidence.Service)
			if !known || !evidence.Current || evidence.Kind != lifecycle.SupportStarts {
				t.Fatalf("expected an unavailable Node.js release: %+v", evidence)
			}
			f.Rule, f.Message, f.LifecycleEvidence = "runtime-lifecycle", message, &evidence
			got := lifecycleApply(t, lifecycleRulesConfig(t, tc.config), f, func(p *policy.DocumentInputs) {
				p.Content = []byte("on: push\njobs: {}\n")
			})
			if len(got) != tc.want {
				t.Fatalf("findings = %+v; want %d unavailable-runtime findings", got, tc.want)
			}
			if tc.want == 1 && (got[0].Message != message || got[0].Severity != "warning") {
				t.Fatalf("unavailable-runtime finding changed: %+v", got[0])
			}
		})
	}
}

func TestRequiredLifecycleCannotBeWeakened(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, config string }{
		{"explicit info", "lint: {rules: {correctness: {runner-support: info}}}"},
		{"rule off", "lint: {rules: {correctness: {runner-support: off}}}"},
		{"lint disabled", "lint: {enabled: false}"},
		{"configured ignore", "ignores: [{rule: runner-support, file: workflow.yml}]"},
		{"inline suppression", "{}"},
		{"regex", "{}"},
		{"selector", "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := lifecycleRulesConfig(t, tc.config)
			f := lifecycleFinding(t, "2026-09-01", true)
			got := lifecycleApply(t, cfg, f, func(p *policy.DocumentInputs) {
				p.EnforcedLifecycle = lifecycleRequired(t)
				if tc.name != "inline suppression" {
					p.Content = []byte("on: push\nruns-on: old\n")
				}
				if tc.name == "regex" {
					var err error
					p.IgnorePatterns, err = configuration.CompileIgnorePatterns([]string{"retirement"})
					if err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "selector" {
					var err error
					p.IgnoreSelectors, err = policy.CompileIgnoreSelectors([]string{"runner-support"})
					if err != nil {
						t.Fatal(err)
					}
				}
			})
			var required *finding.Finding
			for _, item := range got {
				if item.Rule == "runner-support" {
					required = item
				}
			}
			if required == nil || required.Severity != "warning" || required.Lifecycle == nil || len(required.Lifecycle.Policies) != 1 || !required.Lifecycle.Policies[0].Enforced {
				t.Fatalf("required finding lost or weakened: %+v", got)
			}
		})
	}
}

func TestRequiredRemoteLifecycleIsBlocking(t *testing.T) {
	t.Parallel()
	const remote = "https://github.com/example/action/blob/abc/action.yml"
	cfg := lifecycleRulesConfig(t, "{}")
	f := lifecycleFinding(t, "2026-09-01", true)
	f.Path = remote
	got := lifecycleApply(t, cfg, f, func(p *policy.DocumentInputs) {
		p.Content = []byte("on: push\n")
		p.EnforcedLifecycle = lifecycleRequired(t)
	})
	if len(got) != 1 || got[0].NonBlocking {
		t.Fatalf("required remote finding must block: %+v", got)
	}
}

func TestPublicLifecycleMetadataCannotCreateAuthority(t *testing.T) {
	t.Parallel()
	cfg := lifecycleRulesConfig(t, "lint: {rules: {correctness: {runner-support: off}}}")
	f := lifecycleFinding(t, "2026-09-01", true)
	f.LifecycleEvidence = nil
	f.Lifecycle = &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "forged", Enforced: true}}}
	got := lifecycleApply(t, cfg, f, func(p *policy.DocumentInputs) { p.Content = []byte("on: push\nruns-on: old\n") })
	if len(got) != 0 {
		t.Fatalf("untrusted public metadata bypassed rule off: %+v", got)
	}
}

func TestInactiveLifecycleDoesNotUseSuppression(t *testing.T) {
	t.Parallel()
	cfg := lifecycleRulesConfig(t, lifecycleConfig)
	got := lifecycleApply(t, cfg, lifecycleFinding(t, "2026-04-01", false), nil)
	if len(got) != 1 || got[0].Rule != "unused-suppression" {
		t.Fatalf("inactive finding=%+v", got)
	}
}
