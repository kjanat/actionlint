package runnerapi

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

func TestSupportCatalogValidation(t *testing.T) {
	t.Parallel()
	d, err := ParseDate("2026-12-08")
	if err != nil {
		t.Fatal(err)
	}
	good := SupportCatalog{DatasetStatus: DatasetStatus{DatasetID: "dataset", Scope: "repo", Freshness: Fresh, Complete: true}, Policies: []SupportPolicy{{ID: "arm32", Description: "ARM32", URL: "https://example.com/source", Capability: JobExecution, Hosting: []Hosting{SelfHosted}, Architectures: []string{"arm32"}, Changes: []SupportChange{{On: d, State: Unsupported}}}}}
	if err := ValidateSupportCatalog(good, "repo"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SupportCatalog){
		func(c *SupportCatalog) { c.Policies = append(c.Policies, c.Policies[0]) },
		func(c *SupportCatalog) { c.Policies[0].URL = "http://example.com" },
		func(c *SupportCatalog) { c.Policies[0].Hosting = []Hosting{"invalid"} },
		func(c *SupportCatalog) {
			c.Policies[0].Changes = append(c.Policies[0].Changes, c.Policies[0].Changes[0])
		},
		func(c *SupportCatalog) { c.Policies[0].Service.MinVersion = "3.19" },
	} {
		c := CloneSupportCatalog(good)
		mutate(&c)
		if err := ValidateSupportCatalog(c, "repo"); err == nil {
			t.Fatalf("accepted invalid catalogue %+v", c)
		}
	}
}
