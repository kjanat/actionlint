package endpoint

import (
	"errors"
	"net/url"
	"testing"
)

func TestOfCanonicalizesEquivalentSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		want      Authority
		host      string
	}{
		{"https default", "https://Example.COM/", Authority{"https", "example.com", "443"}, "example.com"},
		{"https explicit default", "https://example.com:443/", Authority{"https", "example.com", "443"}, "example.com"},
		{"leading zeros", "https://example.com:0443/", Authority{"https", "example.com", "443"}, "example.com"},
		{"http default", "http://example.com/", Authority{"http", "example.com", "80"}, "example.com"},
		{"http custom", "http://example.com:8080/", Authority{"http", "example.com", "8080"}, "example.com:8080"},
		{"idna", "https://BÜCHER.example/", Authority{"https", "xn--bcher-kva.example", "443"}, "xn--bcher-kva.example"},
		{"trailing dot", "https://example.com./", Authority{"https", "example.com.", "443"}, "example.com."},
		{"ipv4", "https://192.0.2.1:8443/", Authority{"https", "192.0.2.1", "8443"}, "192.0.2.1:8443"},
		{"ipv6", "https://[0:0:0:0:0:0:0:1]:443/", Authority{"https", "::1", "443"}, "[::1]"},
		{"ipv6 zone", "https://[FE80::1%25Ethernet]:8443/", Authority{"https", "fe80::1%Ethernet", "8443"}, "[fe80::1%Ethernet]:8443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Of(u)
			if err != nil || got != tc.want {
				t.Fatalf("Of(%q) = %+v, %v; want %+v", tc.raw, got, err, tc.want)
			}
			if host := got.URLHost(); host != tc.host {
				t.Fatalf("URLHost() = %q, want %q", host, tc.host)
			}
		})
	}
}

func TestOfRejectsUnusableEndpoints(t *testing.T) {
	t.Parallel()
	if _, err := Of(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Of(nil) = %v", err)
	}
	for _, u := range []*url.URL{
		{Scheme: "https", Host: "example.com", User: url.User("user")},
		{Scheme: "https", Opaque: "example.com"},
		{Scheme: "ftp", Host: "example.com"},
		{Scheme: "https", Host: ""},
		{Scheme: "https", Host: "example.com:443:1"},
		{Scheme: "https", Host: "under_score.example"},
		{Scheme: "https", Host: "example.com:65536"},
	} {
		if got, err := Of(u); !errors.Is(err, ErrInvalid) {
			t.Errorf("Of(%#v) = %+v, %v; want ErrInvalid", u, got, err)
		}
	}
}

func TestHostPortBracketsIPv6(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ host, port, want string }{
		{"example.com", "", "example.com"},
		{"example.com", "5000", "example.com:5000"},
		{"::1", "", "[::1]"},
		{"::1", "5000", "[::1]:5000"},
		{"192.0.2.1", "80", "192.0.2.1:80"},
	} {
		if got := HostPort(tc.host, tc.port); got != tc.want {
			t.Errorf("HostPort(%q, %q) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestPortRejectsNonNumbers(t *testing.T) {
	t.Parallel()
	for _, port := range []string{"", "-1", "http", "65536"} {
		if _, err := Port(port); !errors.Is(err, ErrInvalid) {
			t.Errorf("Port(%q) = %v, want ErrInvalid", port, err)
		}
	}
}
