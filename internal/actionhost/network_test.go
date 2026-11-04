package actionhost

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/network"
)

func TestMetadataProtectsReservedPipelineFetches(t *testing.T) {
	ctx, session := network.TrackRequests(t.Context())
	prototype, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/example/action", nil)
	if err != nil {
		t.Fatal(err)
	}
	prototype.Header.Set("Authorization", "Bearer fixture-token")
	reservation, err := session.ReserveQuota(prototype, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	calls := 0
	client, err := New(Options{Mode: Online, Token: "fixture-token", Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := "{}"
		switch r.URL.Path {
		case "/rate_limit":
			body = fmt.Sprintf(`{"resources":{"core":{"remaining":5,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		case "/repos/example/action":
			body = `{"id":42,"full_name":"example/action"}`
		case "/critical":
		default:
			t.Fatalf("optional acquisition spent reserved quota: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Lookup(ctx, ref(t, "example/action@"+sha))
	if err != nil || manifest.Available() || calls != 2 {
		t.Fatalf("manifest=%+v error=%v calls=%d", manifest, err, calls)
	}
	for range 4 {
		r := prototype.Clone(reservation.Context(ctx))
		r.URL.Path = "/critical"
		response, err := client.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if calls != 6 {
		t.Fatalf("pipeline fetches=%d, want 6 total transport attempts", calls)
	}
}

func TestMetadataStopsBeforeAnExhaustedBatch(t *testing.T) {
	ctx, _ := network.TrackRequests(t.Context())
	calls := 0
	client, err := New(Options{Mode: Online, Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/rate_limit" {
			t.Fatal("exhausted metadata query reached network")
		}
		body := fmt.Sprintf(`{"resources":{"core":{"remaining":0,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		result, err := client.Lookup(ctx, ref(t, "example/action@"+sha))
		if err != nil || result.Available() {
			t.Fatalf("%+v %v", result, err)
		}
	}
	if calls != 1 {
		t.Fatalf("quota probe repeated %d times", calls)
	}
}
