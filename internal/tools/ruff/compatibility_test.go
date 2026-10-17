package ruff

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestCompatibleVersion(t *testing.T) {
	for _, tc := range []struct {
		output string
		ok     bool
	}{
		{"ruff 0.17.0", true}, {"ruff 0.17.1", true}, {"ruff 0.18.0", true},
		{"ruff 1.0.0", true}, {"ruff 0.17.0+build.1", true},
		{"ruff 0.17.0 (abc123 2026-10-01)", true}, {"ruff 0.18.0-dev", true},
		{"ruff 0.16.9", false}, {"ruff 0.9.0", false}, {"ruff 0.17.0-dev", false},
		{"ruff 00.17.0", false}, {"ruff unknown", false}, {"other 0.17.0", false},
		{"", false}, {"ruff 0.17.0junk", false},
	} {
		if err := checkVersion(tc.output); (err == nil) != tc.ok {
			t.Errorf("version %q: %v, want compatible=%v", tc.output, err, tc.ok)
		}
	}
}

func TestCompatibilitySharedProbe(t *testing.T) {
	for _, version := range []string{"ruff 0.16.0", "ruff 0.17.0", "unexpected"} {
		var check Compatibility
		probes, warnings := 0, 0
		run := func(args []string, stdin string, callback func([]byte, error) error) {
			probes++
			if !slices.Equal(args, []string{"--version"}) || stdin != "" {
				t.Errorf("unexpected probe: %v, %q", args, stdin)
			}
			if err := callback([]byte(version), nil); err != nil {
				t.Error(err)
			}
		}
		var workers sync.WaitGroup
		for range 16 {
			workers.Go(func() {
				available, err := check.available(t.Context(), run, func() error { return nil }, func(error) { warnings++ })
				if err != nil || available != (version == "ruff 0.17.0") {
					t.Errorf("availability=%v, error=%v", available, err)
				}
			})
		}
		workers.Wait()
		if probes != 1 || warnings != boolCount(version != "ruff 0.17.0") {
			t.Fatalf("probes=%d, warnings=%d", probes, warnings)
		}
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestOptionalVersionOnlyForEligibleScripts(t *testing.T) {
	for _, tc := range []struct {
		shell  string
		config Config
	}{{shell: "bash"}, {shell: "${{ inputs.shell }}"}, {shell: "python", config: Config{Enabled: new(false)}}} {
		checker := New(func([]string, string, func([]byte, error) error) { t.Fatal("disabled checker was executed") }, func() error { return nil }, nil)
		checker.OptionalVersion(&Compatibility{}, t.Context(), func(error) { t.Fatal("disabled checker warned") })
		if err := checker.Check("print(1)", &tc.shell, "test", tc.config, func(Diagnostic) {}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompatibilityCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var check Compatibility
	available, err := check.available(ctx, func(_ []string, _ string, callback func([]byte, error) error) {
		cancel()
		if err := callback(nil, context.Canceled); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, func(error) { t.Fatal("cancellation became a warning") })
	if available || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation swallowed: available=%v, error=%v", available, err)
	}
}

func TestCompatibilityProbeFailure(t *testing.T) {
	var check Compatibility
	warning := ""
	available, err := check.available(t.Context(), func(_ []string, _ string, callback func([]byte, error) error) {
		if err := callback(nil, errors.New("cannot run probe")); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, func(err error) { warning = err.Error() })
	if available || err != nil || !strings.Contains(warning, "cannot run probe") {
		t.Fatalf("probe failure handling: available=%v, error=%v, warning=%s", available, err, warning)
	}
}
