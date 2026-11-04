package network_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"actionlint.kjanat.dev/v2/network"
)

// exampleTransport supplies an offline response; no server is contacted.
type exampleTransport struct{}

func (exampleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func ExampleRequestTracker() {
	ctx, requests := network.TrackRequests(context.Background())
	ctx = network.WithRequestOptions(ctx, network.RequestOptions{Purpose: "dependency", Priority: network.Required})
	client := &http.Client{Transport: network.TrackHTTPRequests(exampleTransport{})}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.org/artifact", nil)
	if err != nil {
		panic(err)
	}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}
	response.Body.Close()
	for _, group := range requests.Statistics().Requests {
		fmt.Println(group.Host, group.Credentials, group.RequestKind, group.Attempts)
	}
	// Output: example.org absent http 1
}
