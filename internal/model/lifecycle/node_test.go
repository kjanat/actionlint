package lifecycle

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/services"
)

func TestNodeLifecycleReleaseSchedule(t *testing.T) {
	t.Parallel()
	date, err := ParseDate("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	service, err := services.Parse("ghes", "3.18")
	if err != nil {
		t.Fatal(err)
	}
	// These dates come from the pinned nodejs/Release schedule embedded beside this test.
	for _, tc := range []struct {
		selector, name, deadline string
		current                  bool
	}{
		{"12", "node12", "2022-04-30", true},
		{"16.x", "node16", "2023-09-11", true},
		{"v22.1.0", "node22", "2027-04-30", false},
		{"26", "node26", "2029-04-30", false},
	} {
		e, message, ok := NodeLifecycle(tc.selector, date, service)
		if !ok || e.Name != tc.name || e.Effective.String() != tc.deadline || e.Current != tc.current || e.Subject != LanguageRuntimes || e.Kind != SupportEnds || !e.Announced.IsZero() || e.Service != service || !strings.Contains(message, tc.deadline) {
			t.Errorf("%s: %+v %s", tc.selector, e, message)
		}
	}
	for _, selector := range []string{"lts/*", ">=20", "20 || 22", "20.0.0-beta", "20.1.2.3", "020", "v", "999", "0.12", "20.x.1", "${{ matrix.node }}", ""} {
		if e, _, ok := NodeLifecycle(selector, date, service); ok {
			t.Errorf("ambiguous or unknown selector %q acquired evidence: %+v", selector, e)
		}
	}
}

func TestNodeLifecycleIntroductionAndEndBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		date      string
		kind      EventKind
		current   bool
		effective string
	}{
		{"2026-05-04", SupportStarts, true, "2026-05-05"},
		{"2026-05-05", SupportEnds, false, "2029-04-30"},
		{"2029-04-29", SupportEnds, false, "2029-04-30"},
		{"2029-04-30", SupportEnds, true, "2029-04-30"},
	} {
		date, err := ParseDate(tc.date)
		if err != nil {
			t.Fatal(err)
		}
		e, _, ok := NodeLifecycle("26", date, services.Service{})
		if !ok || e.Kind != tc.kind || e.Current != tc.current || e.Effective.String() != tc.effective {
			t.Errorf("%s: %+v", tc.date, e)
		}
	}
}
