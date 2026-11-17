package lifecycle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDateRoundTripAndValidation(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"2026-2-01", "2026-02-29", "2026-11-31", "2026-12-08T00:00:00Z"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	a, err := ParseDate("2026-12-08")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDate("2026-12-09")
	if err != nil {
		t.Fatal(err)
	}
	if a.Compare(b) >= 0 || DateOf(time.Date(2026, 12, 8, 23, 0, 0, 0, time.FixedZone("west", -3600))) != b {
		t.Fatal("date ordering or UTC conversion failed")
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Date
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != a {
		t.Fatalf("roundtrip=%v error=%v", decoded, err)
	}
}
