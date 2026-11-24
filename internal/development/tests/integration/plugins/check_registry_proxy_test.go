package plugins_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"golang.org/x/net/http/httpproxy"
)

func TestCheckDefaultRegistryThroughProxy(t *testing.T) {
	const hostname = "plugins.actionlint.kjanat.dev"
	const authority = hostname + ":443"
	const trustFlag = "--trust-default-plugin-registry"
	for _, tc := range []struct {
		name         string
		flags        []string
		noProxy      string
		declare      bool
		wrongTLSName bool
		wantCode     int
		wantError    string
		wantConnect  bool
		wantStatuses bool
	}{
		{name: "explicit trust", flags: []string{trustFlag}, wantConnect: true, wantStatuses: true},
		{name: "proxy alone", wantCode: 3, wantError: "actionlint check " + trustFlag},
		{name: "NO_PROXY", flags: []string{trustFlag}, noProxy: hostname, wantCode: 3, wantError: "fixture cannot resolve destination"},
		{name: "repository declaration", flags: []string{trustFlag}, declare: true, wantCode: 3, wantError: "HTTP acquisition DNS lookup failed"},
		{name: "wrong TLS hostname", flags: []string{trustFlag}, wrongTLSName: true, wantConnect: true, wantCode: 3, wantError: "x509: certificate is valid for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck")
			origin := registry.Connections().Transport.(*http.Transport)
			repo := t.TempDir()
			t.Chdir(repo)
			if out, err := gitcommand.Command(t.Context(), "git", repo, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
			config := "tools: {ruff: {enabled: false}}\n"
			if tc.declare {
				config += "plugins: [{name: github.com/kjanat/actionlint-plugin-shellcheck, version: v1.0.0, registry: " + pluginreference.DefaultRegistry + "}]\n"
			}
			for name, body := range map[string]string{
				".github/actionlint.yaml":     config,
				".github/workflows/check.yml": "on: push\npermissions: {}\njobs: {check: {runs-on: ubuntu-latest, steps: [{uses: ./local}]}}\n",
				"local/action.yml":            "name: Local\ndescription: Local action\nruns: {using: docker, image: 'docker://alpine:3.22'}\n",
			} {
				if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			tlsConfig := origin.TLSClientConfig.Clone()
			dialOrigin := origin.DialContext
			if tc.wrongTLSName {
				mismatch := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("request reached a TLS server with the wrong certificate hostname")
					http.NotFound(w, r)
				}))
				t.Cleanup(mismatch.Close)
				tlsConfig.RootCAs = x509.NewCertPool()
				tlsConfig.RootCAs.AddCert(mismatch.Certificate())
				dialOrigin = func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, mismatch.Listener.Addr().String())
				}
			}
			proxy, connects := registryCONNECTProxy(t, authority, dialOrigin)
			proxyFunc := (&httpproxy.Config{HTTPSProxy: proxy.URL, NoProxy: "github.com," + tc.noProxy}).ProxyFunc()
			transport := &http.Transport{
				Proxy:           func(r *http.Request) (*url.URL, error) { return proxyFunc(r.URL) },
				TLSClientConfig: tlsConfig,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					switch addr {
					case proxy.Listener.Addr().String():
						return (&net.Dialer{}).DialContext(ctx, network, addr)
					case authority:
						return nil, errors.New("fixture cannot resolve destination")
					default:
						return origin.DialContext(ctx, network, addr)
					}
				},
			}
			t.Cleanup(transport.CloseIdleConnections)
			connections := acquisitionhttp.Connections{
				Transport: transport,
				Resolver:  fixtureregistry.Resolver(hostname),
			}
			var out, log bytes.Buffer
			cmd := cli.Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log, Acquisition: dependencycache.Acquisition{
				UserCacheDir: t.TempDir(), Connections: connections,
			}}
			args := append([]string{"actionlint", "check"}, tc.flags...)
			status := cmd.MainContext(t.Context(), append(args, "."))
			if status != tc.wantCode || !strings.Contains(log.String(), tc.wantError) {
				t.Fatalf("status=%d, want %d and stderr containing %q\nstdout: %s\nstderr: %s", status, tc.wantCode, tc.wantError, &out, &log)
			}
			requests := registry.Requests()
			if count := connects.Load(); (count > 0) != tc.wantConnect {
				t.Errorf("CONNECTs=%d, want connections=%t", count, tc.wantConnect)
			}
			if got := slices.Contains(requests, pluginreference.DefaultRegistry+"statuses.json"); got != tc.wantStatuses {
				t.Errorf("statuses.json requested=%t, want %t, registry requests=%v", got, tc.wantStatuses, requests)
			}
		})
	}
}

func registryCONNECTProxy(t *testing.T, authority string, dial func(context.Context, string, string) (net.Conn, error)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != authority {
			http.Error(w, "unexpected proxy destination", http.StatusBadGateway)
			t.Errorf("unexpected proxy request: %s %s", r.Method, r.Host)
			return
		}
		connects.Add(1)
		upstream, err := dial(r.Context(), "tcp", r.Host)
		if err != nil {
			http.Error(w, "could not connect to registry fixture", http.StatusBadGateway)
			t.Error(err)
			return
		}
		defer upstream.Close()
		client, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := buffered.Flush(); err != nil {
			t.Error(err)
			return
		}
		go func() { _, _ = io.Copy(upstream, buffered) }()
		_, _ = io.Copy(client, upstream)
	}))
	t.Cleanup(proxy.Close)
	return proxy, &connects
}
