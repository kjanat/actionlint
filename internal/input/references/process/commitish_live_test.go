package process

import (
	"os"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
)

// This optional comparison exercises the independently built Rust adapter and
// real GitHub requests. Ordinary Go tests do not need the adapter or credentials.
func TestCommitishReportsActualRESTAttempts(t *testing.T) {
	t.Parallel()
	executable := os.Getenv("ACTIONLINT_TEST_COMMITISH_RESOLVER")
	const tokenEnv = "ACTIONLINT_TEST_COMMITISH_TOKEN"
	if executable == "" || os.Getenv(tokenEnv) == "" {
		t.Skip("set ACTIONLINT_TEST_COMMITISH_RESOLVER and ACTIONLINT_TEST_COMMITISH_TOKEN for the live GitHub comparison")
	}
	ctx, requests := accounting.TrackRequests(t.Context())
	process := NewProcess(executable, []string{"--token-env", tokenEnv}, 30*time.Second)
	result, err := process.Resolve(ctx, references.Request{
		Version: 1, Repository: "kjanat/commitish",
		Ref: "40eac37e9505263f0a2ecf0900182d0d11449f19", Kind: "action",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Network == nil || !result.Network.AllRequestsReported || len(result.Network.Requests) != 1 {
		t.Fatalf("incomplete adapter request report: %+v", result.Network)
	}
	request := result.Network.Requests[0]
	if request.Host != "api.github.com" || request.RequestKind != "rest" || request.Credentials != "provided" || request.Attempts != 3 || request.Failures != 0 {
		t.Fatalf("unexpected adapter requests: %+v", request)
	}
	var attempts, reported, unreported, resolverCalls uint64
	for _, row := range requests.Statistics().Requests {
		attempts += row.Attempts
		reported += row.ResolverReportedAttempts
		unreported += row.UnreportedResolverCalls
		resolverCalls += row.ResolverCalls
	}
	if attempts != 3 || reported != 3 || unreported != 0 || resolverCalls != 1 {
		t.Fatalf("host counts: attempts=%d reported=%d unreported=%d resolver_calls=%d", attempts, reported, unreported, resolverCalls)
	}
}
