package acquisitionhttp

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestOfflineStopsBeforeDNSAndTransport(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	ctx, tracker := accounting.TrackRequests(ctx)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.org/config.toml", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := newTransport(request.URL, Grant{}, &http.Transport{}, func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("offline request reached DNS")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if response != nil {
		defer response.Body.Close()
	}
	if response != nil || !errors.Is(err, network.ErrOffline) {
		t.Fatalf("response=%v error=%v", response, err)
	}
	if requests := tracker.Statistics().Requests; len(requests) != 0 {
		t.Fatalf("blocked request counted as sent: %+v", requests)
	}
}
