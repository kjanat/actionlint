package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestRemoteWorkflowPermissionsCLIOnlineAndOffline(t *testing.T) {
	t.Setenv("GH_TOKEN", "fixture-secret")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	var requests []string
	downloads := 0
	transport := actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "raw.githubusercontent.com" || r.URL.Path != "/example/workflows/"+sha+"/.github/workflows/build.yml" {
			t.Fatalf("unexpected HTTP request %s", r.URL)
		}
		requests = append(requests, r.URL.Host+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatal("GitHub credential was not forwarded")
		}
		downloads++
		body := "on: workflow_call\npermissions: {contents: write}\njobs: {publish: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: transport}}
	// GitHub rejects a reusable workflow requesting more token permissions than
	// its caller grants. https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations
	source := "on: push\npermissions: {}\njobs:\n  allowed:\n    permissions: {contents: write}\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n  denied:\n    permissions: {contents: read}\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n  inherited:\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n"
	for _, mode := range []string{"online", "offline"} {
		before := len(requests)
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log, Acquisition: acquisition}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config", "--output-format=json", "--summary=false", "--network-stats", "--" + mode, "-"})
		var result resultreport.CheckResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("%s: status=%d error=%v stderr=%s stdout=%s", mode, status, err, &log, &out)
		}
		wantStatus, wantFindings := 1, 2
		if status != wantStatus || len(result.Diagnostics) != wantFindings {
			t.Fatalf("%s: status=%d findings=%+v stderr=%s", mode, status, result.Diagnostics, &log)
		}
		for _, finding := range result.Diagnostics {
			if finding.Rule != "workflow-call" || !strings.Contains(finding.Message, "requires") || !strings.Contains(finding.Message, "contents: write") || (finding.Start.Line != 9 && finding.Start.Line != 11) {
				t.Fatalf("%s: unexpected finding %+v", mode, finding)
			}
		}
		want := map[string]int{}
		if mode == "online" {
			want = map[string]int{"raw.githubusercontent.com/example/workflows/" + sha + "/.github/workflows/build.yml": 1}
		}
		assertMetadataRequests(t, requests[before:], want)
		var statistics struct {
			Network accounting.Statistics `json:"network"`
		}
		if err := json.Unmarshal(log.Bytes(), &statistics); err != nil {
			t.Fatal(err)
		}
		var counted uint64
		for _, group := range statistics.Network.Requests {
			counted += group.Attempts
			if group.Attempts != 0 && (group.Host != "raw.githubusercontent.com" || group.Credentials != accounting.CredentialsProvided || group.RequestKind != accounting.HTTP || group.Purpose != "workflow-metadata") {
				t.Fatalf("wrong workflow request accounting: %+v", group)
			}
		}
		if counted != uint64(len(requests)-before) {
			t.Fatalf("counted %d requests, made %d", counted, len(requests)-before)
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
	t.Parallel()
	const revision = "ed1f1d971d3603817c62e17f550080ba63eac4c3"
	target, err := os.ReadFile("internal/cli/testdata/remote-workflows/actionlint-nix.yml")
	if err != nil {
		t.Fatal(err)
	}
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "raw.githubusercontent.com" || r.URL.Path != "/kjanat/actionlint/"+revision+"/.github/workflows/nix.yml" {
			t.Fatalf("unexpected HTTP request %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(string(target))), Request: r}, nil
	})}}
	for _, permissions := range []string{"{contents: read}", "{}"} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\npermissions: " + permissions + "\njobs:\n  nix:\n    uses: kjanat/actionlint/.github/workflows/nix.yml@" + revision + "\n"), Stdout: &out, Stderr: &log, Acquisition: acquisition}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config", "--output-format=json", "--summary=false", "-"})
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
	t.Parallel()
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
			t.Parallel()
			sha := strings.Repeat("a", 40)
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "raw.githubusercontent.com" || r.URL.Path != "/example/workflows/"+sha+"/.github/workflows/build.yml" {
					t.Fatalf("unexpected HTTP request %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(target)), Request: r}, nil
			})}}
			// No caller permissions are known. Argument and output checking must
			// still run without inventing a token-permission mismatch.
			source := "on: push\njobs:\n  call:\n    uses: example/workflows/.github/workflows/build.yml@" + sha + "\n" + tc.arguments + "  consume:\n    needs: call\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        env:\n          RESULT: ${{ needs.call.outputs." + tc.output + " }}\n"
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("lint: {rules: {security: {missing-permissions: off}}}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader(source), Stdout: &out, Stderr: &log, Acquisition: acquisition}
			status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", config, "--output-format=json", "--summary=false", "--tool=shellcheck=", "--no-tool=ruff", "-"})
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
