package workspace

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestConfigLoadedCallbackCanReenterSession(t *testing.T) {
	var session *Session
	var calls int
	var reentered *Selection
	var reentryErr error
	session, err := New(Options{OnConfigLoaded: func(repoconfig.Report) {
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
