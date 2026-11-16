package workspace

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
)

func TestConfigLoadedCallbackCanReenterSession(t *testing.T) {
	t.Parallel()
	var session *InputPreparer
	var calls int
	var reentered *PreparedBatch
	var reentryErr error
	session, err := New(t.Context(), Options{OnConfigLoaded: func(configreport.Report) {
		calls++
		if calls > 1 {
			t.Error("reentry invoked the callback again")
			return
		}
		// Fail without leaving a blocked reentrant call behind when the lock is held.
		if !session.configState.TryLock() {
			t.Error("OnConfigLoaded holds the configuration cache lock")
			return
		}
		session.configState.Unlock()
		reentered, reentryErr = session.ReadStdin(strings.NewReader(`on: push
jobs: {}
`), false)
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ReadStdin(strings.NewReader(`on: push
jobs: {}
`), false)
	if err != nil || result == nil || len(result.Sources) != 1 {
		t.Fatalf("outer analysis: %+v, %v", result, err)
	}
	if reentryErr != nil || reentered == nil || len(reentered.Sources) != 1 {
		t.Fatalf("reentrant analysis: %+v, %v", reentered, reentryErr)
	}
	if calls != 1 {
		t.Fatalf("callback invoked %d times for the same project", calls)
	}
}
