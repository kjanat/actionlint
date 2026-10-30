package process

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.17.0", "0.17.0", 0},
		{"0.17.1", "0.17.0", 1},
		{"0.18", "0.17.9", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.17.0-rc1", "0.17.0", -1},
		{"0.17.0+build", "0.17.0", 0},
		{"0.17.0-alpha", "0.17.0-alpha.1", -1},
		{"0.17.0-alpha.1", "0.17.0-alpha.beta", -1},
		{"0.17.0-beta.11", "0.17.0-beta.2", 1},
		{"0.11", "0.11.0", 0},
	} {
		got, err := compareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("compare(%s, %s) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, version := range []string{"", "1..0", "v1.0", "1.0-"} {
		if _, err := compareVersions(version, "1.0"); err == nil {
			t.Errorf("parsed %q", version)
		}
	}
}

// The bundled Ruff probe keeps the Ruff adapter's minimum: 0.17.0 releases
// and newer, never a prerelease of the minimum.
func TestRuffVersionProbe(t *testing.T) {
	probe := bundledManifest(t, "ruff").Process.Version
	pattern := regexp.MustCompile(probe.Pattern)
	for output, accepted := range map[string]bool{
		"ruff 0.17.0\n":         true,
		"ruff 0.17.1":           true,
		"ruff 1.0.0+local\n":    true,
		"ruff 0.16.9\n":         false,
		"ruff 0.17.0-rc.1\n":    false,
		"ruff 0.17\n":           false,
		"ruff-lsp 0.17.0\n":     false,
		"Ruff 0.17.0\n":         false,
		"ruff 0.17.0beta\n":     false,
		"something else\n":      false,
		"ruff 00.17.0\n":        false,
		"ruff 0.17.0 (abc)\n":   true,
		"ruff 0.17.0-dev+1 x\n": false,
	} {
		version, err := identifyVersion("ruff", pattern, probe.Minimum, output)
		if accepted != (err == nil) {
			t.Errorf("%q: version=%q err=%v", output, version, err)
		}
	}
}

func TestProbeSharesCompletedResult(t *testing.T) {
	var p probe
	var runs, warnings atomic.Int32
	failure := errors.New("unavailable")
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			_, err := p.run(t.Context(), func() (string, error) {
				runs.Add(1)
				return "", failure
			}, func(error) { warnings.Add(1) })
			if err == nil || err.Error() != failure.Error() {
				t.Errorf("err=%v", err)
			}
		})
	}
	group.Wait()
	if runs.Load() != 1 || warnings.Load() != 1 {
		t.Fatalf("runs=%d warnings=%d", runs.Load(), warnings.Load())
	}
}

func TestProbeRetriesAfterCancellation(t *testing.T) {
	var p probe
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := p.run(ctx, func() (string, error) {
		cancel()
		return "", ctx.Err()
	}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	version, err := p.run(t.Context(), func() (string, error) { return "1.0.0", nil }, nil)
	if err != nil || version != "1.0.0" {
		t.Fatalf("probe not retried: %q %v", version, err)
	}
}
