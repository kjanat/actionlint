package references

import (
	"encoding/json"
	"testing"
)

func TestResultCloneDetachesNetworkAndFetchLog(t *testing.T) {
	t.Parallel()
	original := Result{FetchLog: json.RawMessage(`{"result":"ok"}`), Network: &RequestReport{AllRequestsReported: true, Requests: []RequestCounts{{Host: "example.com", Attempts: 2}}}}
	clone := original.Clone()
	clone.FetchLog[0] = '['
	clone.Network.Requests[0].Host = "changed"
	clone.Network.AllRequestsReported = false
	if original.FetchLog[0] != '{' || original.Network.Requests[0].Host != "example.com" || !original.Network.AllRequestsReported || clone.Network == original.Network {
		t.Fatalf("clone shares mutable data: original=%+v clone=%+v", original, clone)
	}
}
