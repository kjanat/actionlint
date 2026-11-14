package lifecycle

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestAssessmentWindowsAndAnnouncement(t *testing.T) {
	t.Parallel()
	d := func(s string) Date {
		x, e := ParseDate(s)
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	svc, _ := services.Parse("github.com", "")
	p, e := NewPolicy("migration", "Prepare", Match{Subjects: []Subject{Runners}, Events: []EventKind{SupportEnds}}, []Notification{{Before(90), Warning}, {OnEffective(), Error}}, "base.toml")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		as, ann string
		want    Severity
	}{{"2026-09-01", "2026-09-15", ""}, {"2026-09-15", "2026-09-15", Warning}, {"2026-12-01", "", Error}} {
		ev := Evidence{Subject: Runners, Kind: SupportEnds, Service: svc, Effective: d("2026-12-01"), AsOf: d(tc.as)}
		if tc.ann != "" {
			ev.Announced = d(tc.ann)
		}
		got := Assess(ev, []Policy{p})
		if !got.Matched || (len(got.Active) == 0) != (tc.want == "") {
			t.Fatalf("%+v: %+v", tc, got)
		}
		if tc.want != "" && got.Active[0].Severity != tc.want {
			t.Fatalf("%+v", got)
		}
	}
}

func TestCompositeEvidence(t *testing.T) {
	t.Parallel()
	date := func(s string) Date {
		d, e := ParseDate(s)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	p, e := NewPolicy("migration", "Prepare", Match{Subjects: []Subject{Runners}, Events: []EventKind{SupportEnds}}, []Notification{{Starts: Before(90), Severity: Info}, {Starts: OnEffective(), Severity: Error}}, "policy")
	if e != nil {
		t.Fatal(e)
	}
	event := func(deadline string) Evidence {
		return Evidence{Subject: Runners, Kind: SupportEnds, Effective: date(deadline), AsOf: date("2026-10-01")}
	}
	ended, upcoming, outside := event("2026-09-01"), event("2026-11-01"), event("2027-05-01")
	for _, tc := range []struct {
		name      string
		e         Evidence
		matched   bool
		severity  Severity
		decisions int
	}{
		{"all use weakest candidate", Evidence{AllOf: []Evidence{ended, upcoming}}, true, Info, 2},
		{"any uses strongest fact", Evidence{AnyOf: []Evidence{ended, upcoming}}, true, Error, 1},
		{"inactive candidate", Evidence{AllOf: []Evidence{ended, outside}}, true, "", 0},
		{"candidate not matched", Evidence{AllOf: []Evidence{ended, {Subject: ActionRuntimes, Kind: SupportEnds}}}, false, "", 0},
		{"deduplicate contributors", Evidence{AllOf: []Evidence{ended, ended}}, true, Error, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Assess(tc.e, []Policy{p})
			if a.Matched != tc.matched || a.Severity != tc.severity || len(a.Active) != tc.decisions {
				t.Fatalf("assessment=%+v", a)
			}
		})
	}
}

func TestNotificationStartClampsToEarliestDate(t *testing.T) {
	t.Parallel()
	parse := func(s string) Date {
		d, err := ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	p, err := NewPolicy("early", "Early deadline", Match{Subjects: []Subject{Runners}, Events: []EventKind{SupportEnds}}, []Notification{{Starts: Before(3), Severity: Warning}}, "policy")
	if err != nil {
		t.Fatal(err)
	}
	got := Assess(Evidence{Subject: Runners, Kind: SupportEnds, Effective: parse("0000-01-02"), AsOf: parse("0000-01-01")}, []Policy{p})
	if got.Severity != Warning || len(got.Active) != 1 || got.Active[0].StartsOn.String() != "0000-01-01" {
		t.Fatalf("assessment = %+v", got)
	}
}
