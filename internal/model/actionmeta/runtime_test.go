package actionmeta

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestNode20HostedMigration(t *testing.T) {
	t.Parallel()
	service, _ := services.ParseService("github.com", "")
	date, _ := lifecycle.ParseDate("2026-09-23")
	for _, using := range []string{"node20", "NODE20"} {
		problem := RuntimeProblem(using, LifecycleContext{Service: service, AsOf: date})
		code, message := problem.Code, problem.Message
		if code != CodeRuntimeDeprecated {
			t.Errorf("runtime %s: code=%q, want %q", using, code, CodeRuntimeDeprecated)
		}
		for _, want := range []string{"github.com", "Node 24", "no longer supports the Node 20 opt-out"} {
			if !strings.Contains(message, want) {
				t.Errorf("runtime %s: wanted %q in %q", using, want, message)
			}
		}
		if strings.Contains(message, "scheduled") || strings.Contains(message, "no longer bundled") {
			t.Errorf("runtime %s: conflated hosted migration with bundle inventory: %s", using, message)
		}
	}
}

func TestRuntimeProblemCodeAndMessage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		using   string
		code    ProblemCode
		message string
	}{
		{"node20", CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE20", CodeRuntimeDeprecated, `use "node24" instead`},
		{"node16", CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE16", CodeRuntimeDeprecated, `use "node24" instead`},
		{"node12", CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE12", CodeRuntimeDeprecated, `use "node24" instead`},
		{"node24", "", ""},
		{"NODE24", "", ""},
		{"node999", "", ""},
		{"", "", ""},
	} {
		t.Run(tc.using, func(t *testing.T) {
			t.Parallel()
			problem := RuntimeProblem(tc.using, LifecycleContext{})
			code, message := problem.Code, problem.Message
			if code != tc.code {
				t.Fatalf("code=%q message=%q; want code %q", code, message, tc.code)
			}
			if tc.message == "" {
				if message != "" {
					t.Fatalf("no-finding code has message %q", message)
				}
			} else if !strings.Contains(message, tc.message) {
				t.Fatalf("code=%q message=%q; want message containing %q", code, message, tc.message)
			}
		})
	}
}

func TestRuntimeSupportEvidenceUsesServiceAndDate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		service, date  string
		current, dated bool
	}{
		{"github.com", "2026-08-24", false, true},
		{"github.com", "2026-08-25", false, true},
		{"github.com", "2026-09-22", false, true},
		{"github.com", "2026-09-23", true, true},
		{"github-data-residency", "2026-09-23", true, true},
		{"ghes", "2026-09-23", true, false},
		{"unknown", "2026-09-23", true, false},
	} {
		service, err := services.ParseService(tc.service, "")
		if err != nil {
			t.Fatal(err)
		}
		date, err := lifecycle.ParseDate(tc.date)
		if err != nil {
			t.Fatal(err)
		}
		problem := RuntimeProblem("node20", LifecycleContext{Service: service, AsOf: date})
		e := problem.Evidence
		if e == nil || e.Current != tc.current || (!e.Effective.IsZero()) != tc.dated || e.Subject != lifecycle.ActionRuntimes || e.AsOf != date {
			t.Fatalf("%s %s: %+v", tc.service, tc.date, e)
		}
		if tc.dated && (e.Announced.String() != "2026-08-25" || e.Effective.String() != "2026-09-23" || e.Kind != lifecycle.SupportEnds) {
			t.Fatalf("incorrect support dates: %+v", e)
		}
		if !tc.dated && (e.Kind != lifecycle.RuntimeReplaced || strings.Contains(problem.Message, "opt-out")) {
			t.Fatalf("cloud policy leaked: %+v", problem)
		}
	}
	if problem := RuntimeProblem("node26", LifecycleContext{}); problem.Code != "" || problem.Evidence != nil {
		t.Fatalf("invented action runtime: %+v", problem)
	}
}
