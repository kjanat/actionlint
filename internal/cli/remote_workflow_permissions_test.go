package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestRemoteWorkflowPermissionsCLIOnlineAndOffline(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("GH_TOKEN", "fixture-secret")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	requests, downloads := 0, 0
	original := http.DefaultTransport
	http.DefaultTransport = actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatal("GitHub credential was not forwarded")
		}
		body := ""
		switch r.URL.Path {
		case "/rate_limit":
			body = fmt.Sprintf(`{"resources":{"core":{"remaining":100,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		case "/repos/example/workflows":
			body = `{"id":42,"full_name":"example/workflows"}`
		case "/repos/example/workflows/git/commits/" + sha:
			body = `{"sha":"` + sha + `"}`
		case "/repos/example/workflows/contents/.github/workflows/build.yml":
			downloads++
			if r.URL.Query().Get("ref") != sha {
				t.Fatal("workflow request was not pinned to a commit")
			}
			body = "on: workflow_call\npermissions: {contents: write}\njobs: {publish: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
		default:
			t.Fatalf("unexpected HTTP request %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	// GitHub rejects a reusable workflow requesting more token permissions than
	// its caller grants. https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations
	source := "on: push\npermissions: {}\njobs:\n  allowed:\n    permissions: {contents: write}\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n  denied:\n    permissions: {contents: read}\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n  inherited:\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n"
	for _, mode := range []string{"online", "offline", "off"} {
		before := requests
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output-format=json", "--summary=false", "--network-stats", "--action-metadata=" + mode, "-"})
		var result resultreport.CheckResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("%s: status=%d error=%v stderr=%s stdout=%s", mode, status, err, &log, &out)
		}
		wantStatus, wantFindings := 1, 2
		if mode == "off" {
			wantStatus, wantFindings = 0, 0
		}
		if status != wantStatus || len(result.Diagnostics) != wantFindings {
			t.Fatalf("%s: status=%d findings=%+v stderr=%s", mode, status, result.Diagnostics, &log)
		}
		for _, finding := range result.Diagnostics {
			if finding.Rule != "workflow-call" || !strings.Contains(finding.Message, "requires") || !strings.Contains(finding.Message, "contents: write") || (finding.Start.Line != 9 && finding.Start.Line != 11) {
				t.Fatalf("%s: unexpected finding %+v", mode, finding)
			}
		}
		if mode != "online" && requests != before {
			t.Fatalf("%s made %d network requests", mode, requests-before)
		}
		var statistics struct {
			Network accounting.Statistics `json:"network"`
		}
		if err := json.Unmarshal(log.Bytes(), &statistics); err != nil {
			t.Fatal(err)
		}
		var counted uint64
		for _, group := range statistics.Network.Requests {
			counted += group.Attempts
			if group.Host != "api.github.com" || group.Credentials != accounting.CredentialsProvided || group.RequestKind != accounting.REST || (group.Purpose != "workflow-metadata" && group.Purpose != "quota-preflight") {
				t.Fatalf("wrong workflow request accounting: %+v", group)
			}
		}
		if counted != uint64(requests-before) {
			t.Fatalf("counted %d requests, made %d", counted, requests-before)
		}
		if strings.Contains(out.String()+log.String(), "fixture-secret") {
			t.Fatal("credential leaked")
		}
	}
	if downloads != 1 {
		t.Fatalf("downloaded one reusable workflow %d times", downloads)
	}
}

func TestRemoteWorkflowPermissionsRealWorkflow(t *testing.T) {
	const revision = "ed1f1d971d3603817c62e17f550080ba63eac4c3"
	target, err := os.ReadFile("internal/cli/testdata/remote-workflows/actionlint-nix.yml")
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	original := http.DefaultTransport
	http.DefaultTransport = actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/rate_limit":
			body = fmt.Sprintf(`{"resources":{"core":{"remaining":100,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		case "/repos/kjanat/actionlint":
			body = `{"id":42,"full_name":"kjanat/actionlint"}`
		case "/repos/kjanat/actionlint/git/commits/" + revision:
			body = `{"sha":"` + revision + `"}`
		case "/repos/kjanat/actionlint/contents/.github/workflows/nix.yml":
			body = string(target)
		default:
			t.Fatalf("unexpected HTTP request %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, permissions := range []string{"{contents: read}", "{}"} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\npermissions: " + permissions + "\njobs:\n  nix:\n    uses: kjanat/actionlint/.github/workflows/nix.yml@" + revision + "\n"), Stdout: &out, Stderr: &log}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output-format=json", "--summary=false", "-"})
		var result resultreport.CheckResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("status=%d error=%v stderr=%s stdout=%s", status, err, &log, &out)
		}
		if permissions == "{}" {
			if status != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "workflow-call" || !strings.Contains(result.Diagnostics[0].Message, `nested job "check"`) || !strings.Contains(result.Diagnostics[0].Message, "contents: read") {
				t.Fatalf("missing real callee requirement: %d %+v", status, result.Diagnostics)
			}
		} else if status != 0 || len(result.Diagnostics) != 0 {
			t.Fatalf("valid real call rejected: %d %+v", status, result.Diagnostics)
		}
	}
}

func TestRemoteWorkflowArgumentsAndOutputs(t *testing.T) {
	const target = `on:
  workflow_call:
    inputs:
      label: {type: string, required: true}
    secrets:
      token: {required: true}
    outputs:
      result: {value: '${{ jobs.build.outputs.result }}'}
permissions: {contents: write}
jobs:
  build:
    runs-on: ubuntu-latest
    outputs: {result: ok}
    steps: [{run: echo ok}]
`
	for _, tc := range []struct {
		name, arguments, output, message string
	}{
		{"valid", "    with: {label: hello}\n    secrets: {token: value}\n", "result", ""},
		{"required input", "    secrets: {token: value}\n", "result", `input "label" is required`},
		{"unknown input", "    with: {label: hello, unknown: value}\n    secrets: {token: value}\n", "result", `input "unknown" is not defined`},
		{"required secret", "    with: {label: hello}\n", "result", `secret "token" is required`},
		{"unknown secret", "    with: {label: hello}\n    secrets: {token: value, unknown: value}\n", "result", `secret "unknown" is not defined`},
		{"unknown output", "    with: {label: hello}\n    secrets: {token: value}\n", "missing", `property "missing" is not defined`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := t.TempDir()
			t.Setenv("LOCALAPPDATA", cache)
			t.Setenv("XDG_CACHE_HOME", cache)
			t.Setenv("HOME", cache)
			sha := strings.Repeat("a", 40)
			original := http.DefaultTransport
			http.DefaultTransport = actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch r.URL.Path {
				case "/rate_limit":
					body = fmt.Sprintf(`{"resources":{"core":{"remaining":100,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
				case "/repos/example/workflows":
					body = `{"id":42,"full_name":"example/workflows"}`
				case "/repos/example/workflows/git/commits/" + sha:
					body = `{"sha":"` + sha + `"}`
				case "/repos/example/workflows/contents/.github/workflows/build.yml":
					body = target
				default:
					t.Fatalf("unexpected HTTP request %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			// No caller permissions are known. Argument and output checking must
			// still run without inventing a token-permission mismatch.
			source := "on: push\njobs:\n  call:\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n" + tc.arguments + "  consume:\n    needs: call\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          RESULT: ${{ needs.call.outputs." + tc.output + " }}\n"
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log}
			status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output-format=json", "--summary=false", "--tool=shellcheck=", "-"})
			var result resultreport.CheckResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("status=%d error=%v stderr=%s stdout=%s", status, err, &log, &out)
			}
			if tc.message == "" {
				if status != 0 || len(result.Diagnostics) != 0 {
					t.Fatalf("valid call rejected: %d %+v %s", status, result.Diagnostics, &log)
				}
			} else if status != 1 || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, tc.message) {
				t.Fatalf("wanted %q, got status=%d findings=%+v stderr=%s", tc.message, status, result.Diagnostics, &log)
			}
		})
	}
}
