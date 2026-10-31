package source

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHTTPSReaderPreservesCustomSource(t *testing.T) {
	want := "https://example.test/base.yaml?version=1"
	reader := New(t.Context(), func(name string) ([]byte, error) {
		if name != want {
			t.Fatalf("source = %q", name)
		}
		return []byte("custom"), nil
	})
	reader.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("custom reader bypassed")
		return nil, errors.New("unexpected request")
	})
	content, err := reader.ReadFile(want)
	if err != nil || string(content) != "custom" {
		t.Fatalf("read = %q, %v", content, err)
	}
}

func TestHTTPSReaderLimitsAndRedirects(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		location   string
		want       string
	}{
		{name: "success", body: "tools: {}", status: http.StatusOK},
		{name: "oversized", body: strings.Repeat("x", MaxBytes+1), status: http.StatusOK, want: "exceeds"},
		{name: "status", status: http.StatusNotFound, want: "404"},
		{name: "redirect", status: http.StatusFound, location: "https://example.test/final.yaml", want: "final HTTPS URL"},
		{name: "downgrade", status: http.StatusFound, location: "http://example.test/final.yaml", want: "redirect"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := New(t.Context(), nil)
			calls := 0
			reader.client.Transport = transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Header: http.Header{"Location": {test.location}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: request}, nil
			})
			content, err := reader.ReadFile("https://example.test/base.yaml")
			if test.want == "" {
				if err != nil || string(content) != test.body {
					t.Fatalf("read = %q, %v", content, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if calls != 1 {
				t.Fatalf("requests = %d", calls)
			}
		})
	}
}

func TestHTTPSReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := New(ctx, nil)
	reader.client.Transport = transportFunc(func(request *http.Request) (*http.Response, error) {
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	if _, err := reader.ReadFile("https://example.test/base.yaml"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestReaderDetachesSourceErrors(t *testing.T) {
	retained := &fs.PathError{Op: "read", Path: "original.yaml", Err: fs.ErrNotExist}
	reader := New(t.Context(), func(string) ([]byte, error) { return nil, retained })
	_, err := reader.ReadFile("original.yaml")
	before := err.Error()
	retained.Path = "mutated.yaml"
	if err.Error() != before || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("published error changed or lost classification: %v", err)
	}
	pathError, ok := errors.AsType[*fs.PathError](err)
	if !ok || pathError.Path != "original.yaml" {
		t.Fatalf("detached path error = %v", err)
	}
}
