package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRuntimeSources() map[string][]byte {
	var parser strings.Builder
	for _, version := range []string{"node12", "node16", "node20", "node24"} {
		fmt.Fprintf(&parser, "string.Equals(usingToken.Value, %q, StringComparison.OrdinalIgnoreCase)\n", version)
	}
	return map[string][]byte{
		runtimeSourcePaths[0]: []byte(parser.String()),
		runtimeSourcePaths[1]: []byte(parser.String()),
		runtimeSourcePaths[2]: []byte("NODE20_VERSION=\"20.0.0\"\nNODE24_VERSION=\"24.0.0\"\n"),
		runtimeSourcePaths[3]: []byte(`public static class Constants {
public static class NodeMigration {
public static readonly string Node20DeprecationUrl = "https://github.blog/changelog/example/";
public static readonly string Node20RemovalDate = "September 23rd, 2026";
}
}`),
		runtimeSourcePaths[4]: []byte("public sealed class HandlerFactory {}"),
	}
}

func TestRuntimeLifecycle(t *testing.T) {
	sources := testRuntimeSources()
	runtimes, err := collectRuntimes(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !runtimes["node12"].Removed || !runtimes["node16"].Removed || runtimes["node20"].Removed || !runtimes["node20"].Deprecated || runtimes["node24"].Deprecated {
		t.Fatalf("incorrect lifecycle: %#v", runtimes)
	}
	if _, ok := runtimes["node22"]; ok {
		t.Fatal("invented an accepted metadata version from the Node release sequence")
	}
	if runtimes["node20"].RemovalDate != "September 23rd, 2026" {
		t.Fatal("lost upstream removal date")
	}
	// A future runtime is discovered without changing the generator's version list.
	for _, path := range runtimeSourcePaths[:2] {
		sources[path] = append(sources[path], []byte(`string.Equals(usingToken.Value, "node26", StringComparison.OrdinalIgnoreCase)`)...)
	}
	sources[runtimeSourcePaths[2]] = append(sources[runtimeSourcePaths[2]], []byte("NODE26_VERSION=\"26.1.0\"\n")...)
	runtimes, err = collectRuntimes(sources)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := runtimes["node26"]; !ok || r.Removed || r.Deprecated {
		t.Fatalf("future runtime not recognized: %#v", runtimes)
	}
	// Passing a calendar deadline alone must not invent a binary removal.
	sources[runtimeSourcePaths[3]] = bytes.ReplaceAll(sources[runtimeSourcePaths[3]], []byte("2026"), []byte("2000"))
	runtimes, err = collectRuntimes(sources)
	if err != nil || runtimes["node20"].Removed {
		t.Fatalf("a date was mistaken for packaging evidence: %#v, %v", runtimes, err)
	}
	sources[runtimeSourcePaths[2]] = bytes.ReplaceAll(sources[runtimeSourcePaths[2]], []byte("NODE20_VERSION=\"20.0.0\"\n"), nil)
	runtimes, err = collectRuntimes(sources)
	if err != nil || !runtimes["node20"].Removed {
		t.Fatalf("actual binary removal not recognized: %#v, %v", runtimes, err)
	}
}

func TestRuntimeLifecycleAfterMigrationConstantsAreRemoved(t *testing.T) {
	sources := testRuntimeSources()
	// Runner.Common/Constants.cs no longer defines NodeMigration after the
	// runner removes Node 20. Accepted metadata values still include node20.
	sources[runtimeSourcePaths[3]] = []byte("public static class Constants {}")
	sources[runtimeSourcePaths[2]] = []byte("NODE24_VERSION=\"24.0.0\"\n")
	runtimes, err := collectRuntimes(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !runtimes["node20"].Removed || runtimes["node24"].Removed || runtimes["node24"].Deprecated {
		t.Fatalf("incorrect lifecycle: %#v", runtimes)
	}
	generated, err := generateRuntimes(sources, testRevision)
	if err != nil || !bytes.Contains(generated, []byte(`"node20": {Removed: true`)) {
		t.Fatalf("removed runtime was lost: %s, %v", generated, err)
	}
}

func TestRuntimeExecutionUsesBundledReplacement(t *testing.T) {
	// actions/runner@cf65f3ec8100ee0046d735ac5d647e00f4b8e817:
	// src/Runner.Worker/Handlers/HandlerFactory.cs assigns NodeVersion for
	// every JavaScript action before constructing its handler.
	sources := testRuntimeSources()
	sources[runtimeSourcePaths[3]] = []byte("public static class Constants {}")
	sources[runtimeSourcePaths[2]] = []byte("NODE24_VERSION=\"24.0.0\"\n")
	sources[runtimeSourcePaths[4]] = []byte(`public sealed class HandlerFactory {
else if (data.ExecutionType == ActionExecutionType.NodeJS) {
    handler = HostContext.CreateService<INodeScriptActionHandler>();
    var nodeData = data as NodeJSActionExecutionData;
    nodeData.NodeVersion = "node24";
    (handler as INodeScriptActionHandler).Data = nodeData;
}
}`)
	runtimes, err := collectRuntimes(sources)
	if err != nil {
		t.Fatal(err)
	}
	for name, runtime := range runtimes {
		if runtime.ExecutionRuntime != "node24" {
			t.Errorf("%s executes with %q, want node24", name, runtime.ExecutionRuntime)
		}
	}
	sources[runtimeSourcePaths[2]] = []byte("NODE20_VERSION=\"20.0.0\"\n")
	if _, err := collectRuntimes(sources); err == nil {
		t.Fatal("accepted a replacement runtime that is not bundled")
	}
}

func TestRuntimeExecutionRejectsUnrecognizedAssignments(t *testing.T) {
	for _, assignment := range []string{
		`nodeData.NodeVersion = SelectRuntime();`,
		`if (enabled) { nodeData.NodeVersion = "node24"; }`,
		`nodeData.NodeVersion = "node20"; nodeData.NodeVersion = "node24";`,
	} {
		t.Run(assignment, func(t *testing.T) {
			sources := testRuntimeSources()
			sources[runtimeSourcePaths[4]] = []byte("public sealed class HandlerFactory { else if (data.ExecutionType == ActionExecutionType.NodeJS) {" + assignment + "} }")
			if _, err := collectRuntimes(sources); err == nil {
				t.Fatal("accepted an execution runtime that could not be determined")
			}
		})
	}
}

func TestRuntimeRefreshPreservesOutputOnFailure(t *testing.T) {
	for _, scenario := range []string{"success", "cdn failure", "parser disagreement", "missing packaging", "missing migration", "changed migration syntax", "bad revision"} {
		t.Run(scenario, func(t *testing.T) {
			sources := testRuntimeSources()
			switch scenario {
			case "parser disagreement":
				sources[runtimeSourcePaths[1]] = nil
			case "missing packaging":
				sources[runtimeSourcePaths[2]] = nil
			case "missing migration":
				sources[runtimeSourcePaths[3]] = nil
			case "changed migration syntax":
				sources[runtimeSourcePaths[3]] = bytes.ReplaceAll(sources[runtimeSourcePaths[3]], []byte("public static readonly"), []byte("public const"))
			}
			client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				body, status := "", http.StatusOK
				switch req.URL.Host {
				case "api.github.com":
					if req.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing GitHub authorization")
					}
					body = fmt.Sprintf(`[{"sha":%q,"commit":{"committer":{"date":"2026-09-08T00:00:00Z"}}}]`, testRevision)
					if scenario == "bad revision" {
						body = `[]`
					}
				case "cdn.jsdelivr.net":
					if req.Header.Get("Authorization") != "" {
						t.Error("GitHub token leaked to the CDN")
					}
					prefix := "/gh/" + repository + "@" + testRevision + "/"
					if !strings.HasPrefix(req.URL.Path, prefix) {
						t.Fatalf("download not tied to the resolved revision: %s", req.URL)
					}
					body = string(sources[strings.TrimPrefix(req.URL.Path, prefix)])
					if scenario == "cdn failure" {
						status = http.StatusServiceUnavailable
					}
				default:
					t.Fatalf("unexpected request: %s", req.URL)
				}
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			output := filepath.Join(t.TempDir(), "runtimes.go")
			if err := os.WriteFile(output, []byte("previous data"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := refreshRuntimes(t.Context(), client, "test-token", output)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("unexpected refresh result: %v", err)
			}
			got, readErr := os.ReadFile(output)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err != nil && string(got) != "previous data" {
				t.Fatal("failed refresh destroyed the existing table")
			}
			if err == nil && (!bytes.Contains(got, []byte(testRevision)) || !bytes.Contains(got, []byte("September 23rd, 2026"))) {
				t.Fatal("generated table lost its evidence")
			}
		})
	}
}
