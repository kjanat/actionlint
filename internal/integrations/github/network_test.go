package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestMetadataProtectsReservedPipelineFetches(t *testing.T) {
	t.Parallel()
	for _, rawStatus := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(rawStatus), func(t *testing.T) {
			t.Parallel()
			ctx, session := accounting.TrackRequests(t.Context())
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
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "raw.githubusercontent.com" || r.URL.Host == "cdn.jsdelivr.net" {
					return respond(r, rawStatus, rawText, actionSource), nil
				}
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
				return respond(r, http.StatusOK, "", body), nil
			}}
			client, err := New(Options{Mode: Online, Token: "fixture-token", Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(ctx, ref(t, "example/action@"+sha))
			if err != nil {
				t.Fatal(err)
			}
			wantAvailable, wantCalls := true, 1
			if rawStatus != http.StatusOK {
				wantAvailable, wantCalls = false, 3
			}
			if manifest.Available() != wantAvailable || len(hosts.recorded()) != wantCalls {
				t.Fatalf("manifest=%+v calls=%v", manifest, hosts.recorded())
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
			if len(hosts.recorded()) != wantCalls+4 {
				t.Fatalf("pipeline fetches=%v, want %d total transport attempts", hosts.recorded(), wantCalls+4)
			}
		})
	}
}

func TestMetadataStopsBeforeAnExhaustedBatch(t *testing.T) {
	t.Parallel()
	ctx, _ := accounting.TrackRequests(t.Context())
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "raw.githubusercontent.com" || r.URL.Host == "cdn.jsdelivr.net" {
			return respond(r, http.StatusServiceUnavailable, "", ""), nil
		}
		if r.URL.Path != "/rate_limit" {
			t.Fatal("exhausted metadata query reached network")
		}
		body := fmt.Sprintf(`{"resources":{"core":{"remaining":0,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		return respond(r, http.StatusOK, "", body), nil
	}}
	client, err := New(Options{Mode: Online, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		result, err := client.Lookup(ctx, ref(t, "example/action@"+sha))
		if err != nil || result.Available() {
			t.Fatalf("%+v %v", result, err)
		}
	}
	if hosts.count("api.github.com") != 1 || hosts.count("raw.githubusercontent.com") != 3 || hosts.count("cdn.jsdelivr.net") != 0 {
		t.Fatalf("quota probe repeated: %v", hosts.recorded())
	}
}

func TestAdmitBatchClassifiesQuotaFailures(t *testing.T) {
	t.Parallel()
	ctx, session := accounting.TrackRequests(t.Context())
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: "action-metadata", Priority: accounting.Enrichment, RequestKind: accounting.REST, QuotaPool: "core"})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/example/action", nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing, err := admitBatch(ctx, session, request, 1); missing != nil || err != nil {
		t.Fatalf("unknown quota=%v,%v", missing, err)
	}
	reservation, err := session.ReserveQuota(request, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	if missing, err := admitBatch(ctx, session, request, 1); missing == nil || missing.Reason() != availability.LimitReason || err != nil {
		t.Fatalf("reserved quota=%v,%v", missing, err)
	}
	invalid := request.Clone(ctx)
	invalid.URL = &url.URL{Scheme: "https", Host: "api.github.com:bad"}
	if missing, err := admitBatch(ctx, session, invalid, 1); missing == nil || missing.Reason() != availability.FailedReason || err != nil {
		t.Fatalf("invalid request=%v,%v", missing, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if missing, err := admitBatch(canceled, session, request.Clone(canceled), 1); missing != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled=%v,%v", missing, err)
	}
}
