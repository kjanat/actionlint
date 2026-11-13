package references

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func TestValidateResultComparesKnownRepositoryIDs(t *testing.T) {
	known := func(value uint64) repositoryid.ID {
		id, err := repositoryid.Known(value)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, test := range []struct {
		name               string
		expected, observed repositoryid.ID
		repository         string
		valid              bool
	}{
		{name: "both unknown", repository: "Owner/Repo", valid: true},
		{name: "expected unknown", observed: known(42), repository: "owner/repo", valid: true},
		{name: "observed unknown", expected: known(42), repository: "owner/repo", valid: true},
		{name: "same known", expected: known(42), observed: known(42), repository: "owner/repo", valid: true},
		{name: "different known", expected: known(42), observed: known(43), repository: "owner/repo"},
		{name: "unknown with another name", repository: "owner/other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: test.expected}
			result := Result{Version: 1, RepositoryID: test.observed, Repository: test.repository, Commit: strings.Repeat("a", 40), ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}
			if err := ValidateResult(request, result); (err == nil) != test.valid {
				t.Fatalf("ValidateResult = %v, want valid %t", err, test.valid)
			}
		})
	}
}

func TestResultJSONOmitsUnknownRepositoryID(t *testing.T) {
	data, err := json.Marshal(Result{Version: 1, Repository: "owner/repo", Commit: strings.Repeat("a", 40), ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)})
	if err != nil || strings.Contains(string(data), "repository_id") {
		t.Fatalf("result = %s, %v", data, err)
	}
	data, err = json.Marshal(Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"})
	if err != nil || string(data) != `{"version":1,"repository":"owner/repo","ref":"v1","kind":"action"}` {
		t.Fatalf("request = %s, %v", data, err)
	}
}
