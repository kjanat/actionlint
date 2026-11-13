package availability_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestUnavailableFactsRemainDistinctFromKnownEmpty(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		result availability.String
		reason availability.Reason
	}{
		{availability.String{}, availability.NotFetchedReason},
		{availability.UnavailableString(availability.Offline()), availability.OfflineReason},
		{availability.UnavailableString(availability.LookupDisabled()), availability.LookupDisabledReason},
		{availability.UnavailableString(availability.Denied()), availability.DeniedReason},
		{availability.UnavailableString(availability.Failed()), availability.FailedReason},
		{availability.UnavailableString(availability.NotFound()), availability.NotFoundReason},
		{availability.UnavailableString(availability.UnsupportedHost()), availability.UnsupportedHostReason},
		{availability.UnavailableString(availability.ResolverNotConfigured()), availability.ResolverNotConfiguredReason},
		{availability.UnavailableString(availability.UnsupportedObjectFormat()), availability.UnsupportedObjectFormatReason},
		{availability.UnavailableString(availability.InvalidResolverResult()), availability.InvalidResolverResultReason},
	} {
		if _, known := test.result.Value(); known || test.result.Reason() != test.reason {
			t.Fatalf("unavailable cause was lost: %q", test.result.Reason())
		}
	}
	empty := availability.Available("")
	if value, known := empty.Value(); !known || value != "" || empty.Reason() != "" {
		t.Fatal("known empty value became unavailable")
	}
}
