package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/availability"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestActionMetadataCLIReportsOfflineCoverage(t *testing.T) {
	t.Parallel()
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\npermissions: {}\n"), Stdout: &out, Stderr: &log}
	code := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config", "--offline", "--output-format=json", "--summary=false", "-"})
	if code != 0 {
		t.Fatalf("status=%d stderr=%s output=%s", code, log.String(), out.String())
	}
	var result resultreport.CheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Reason != availability.OfflineReason {
		t.Fatalf("coverage=%v", result.ActionMetadata)
	}
}

func TestSkippedMetadataLookupsDoNotProduceFindings(t *testing.T) {
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("disabled acquisition requested %s", r.URL)
		return nil, errors.New("unexpected network request")
	})}}
	path := filepath.Join(t.TempDir(), "actionlint.yml")
	if err := os.WriteFile(path, []byte("plugins: []\nlint:\n  rules:\n    conventions:\n      missing-action-metadata: error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode, ref string
		reason    availability.Reason
	}{
		{"offline", "v1", availability.OfflineReason},
		{"offline", strings.Repeat("a", 40), availability.CacheMissReason},
	} {
		t.Run(tc.mode+"/"+tc.ref, func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@" + tc.ref + "}]}}\npermissions: {}\n"), Stdout: &out, Stderr: &log, Acquisition: acquisition}
			code := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", path, "--" + tc.mode, "--resolve-with", filepath.Join(t.TempDir(), "missing-resolver"), "--output=json", "--summary=false", "-"})
			if code != 0 {
				t.Fatalf("status=%d stderr=%s stdout=%s", code, &log, &out)
			}
			var result resultreport.CheckResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Reason != tc.reason {
				t.Fatalf("coverage=%+v", result.ActionMetadata)
			}
			for _, finding := range result.Diagnostics {
				if finding.Rule == "missing-action-metadata" {
					t.Fatalf("skipped lookup produced a finding: %+v", finding)
				}
			}
		})
	}
}

func TestStrictDoesNotRequireMetadataOrJobTimeoutWithoutConfiguration(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, settings string
		timeouts       int
	}{
		{"strict", "", 0},
		{"configured timeout", "lint: {rules: {conventions: {job-timeout: on}}}\n", 1},
		{"all stable rules", "lint: {rules: {preset: all}}\n", 1},
		{"configured exclusion", "lint: {rules: {preset: all, conventions: {job-timeout: off}}}\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataFixture(t, sha, "", &requests)}}
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("plugins: []\n"+tc.settings), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, log bytes.Buffer
			command := Command{Stdin: strings.NewReader("on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"), Stdout: &out, Stderr: &log, Acquisition: acquisition}
			code := command.MainContext(t.Context(), []string{"actionlint", "check", "--strict", "--config", config, "--online", "--output=json", "--summary=false", "-"})
			if code != 1 {
				t.Fatalf("status=%d stderr=%s output=%s", code, &log, &out)
			}
			var result resultreport.CheckResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].State != "available" || result.ActionMetadata[0].Commit != sha {
				t.Fatalf("coverage=%v", result.ActionMetadata)
			}
			timeouts, pins := 0, 0
			for _, finding := range result.Diagnostics {
				switch finding.Rule {
				case "missing-action-metadata":
					t.Errorf("available metadata produced %v", finding)
				case "job-timeout":
					timeouts++
				case "unpinned-reference":
					pins++
				default:
					t.Errorf("unexpected finding: %v", finding)
				}
			}
			if timeouts != tc.timeouts || pins != 1 {
				t.Fatalf("timeout findings=%d, want %d; unpinned findings=%d, want 1", timeouts, tc.timeouts, pins)
			}
		})
	}
}
