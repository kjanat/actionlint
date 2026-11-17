package runnerapi

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
)

func TestSupportCatalogValidation(t *testing.T) {
	t.Parallel()
	d, err := lifecycle.ParseDate("2026-12-08")
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
