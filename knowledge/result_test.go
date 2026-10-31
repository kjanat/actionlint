package knowledge_test

import (
	"actionlint.kjanat.dev/v2/knowledge"
	"testing"
)

func TestUnavailableFactsRemainDistinctFromKnownEmpty(t *testing.T) {
	for _, test := range []struct {
		result knowledge.String
		reason knowledge.Reason
	}{
		{knowledge.String{}, knowledge.NotFetchedReason},
		{knowledge.UnavailableString(knowledge.Offline()), knowledge.OfflineReason},
		{knowledge.UnavailableString(knowledge.LookupDisabled()), knowledge.LookupDisabledReason},
		{knowledge.UnavailableString(knowledge.Denied()), knowledge.DeniedReason},
		{knowledge.UnavailableString(knowledge.Failed()), knowledge.FailedReason},
		{knowledge.UnavailableString(knowledge.NotFound()), knowledge.NotFoundReason},
	} {
		if _, known := test.result.Value(); known || test.result.Reason() != test.reason {
			t.Fatalf("unavailable cause was lost: %q", test.result.Reason())
		}
	}
	empty := knowledge.Available("")
	if value, known := empty.Value(); !known || value != "" || empty.Reason() != "" {
		t.Fatal("known empty value became unavailable")
	}
}
