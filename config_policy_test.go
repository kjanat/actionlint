package actionlint

import (
	"math"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestConfigTimeoutBounds(t *testing.T) {
	for _, tc := range []struct {
		input string
		min   float64
		max   float64
	}{
		{"true", 0, 0},
		{"{min-minutes: 5}", 5, 0},
		{"{min-minutes: 1.5, max-minutes: 30}", 1.5, 30},
		{"{max-minutes: 5, min-minutes: 5}", 5, 5},
		{"{min-minutes: 017, max-minutes: 018}", 17, 18},
	} {
		t.Run(tc.input, func(t *testing.T) {
			cfg, err := ParseConfig([]byte("policy: {require-job-timeout: " + tc.input + "}"))
			if err != nil {
				t.Fatal(err)
			}
			p := cfg.RequiresJobTimeout()
			lower, hasMin := p.MinMinutes()
			upper, hasMax := p.MaxMinutes()
			if lower != tc.min || upper != tc.max || hasMin != (tc.min > 0) || hasMax != (tc.max > 0) {
				t.Fatalf("got bounds (%v, %v), enabled (%v, %v)", lower, upper, hasMin, hasMax)
			}
		})
	}
	for _, tc := range []struct{ mapping, message string }{
		{"{min-minutes: 0}", "must be greater than zero"},
		{"{min-minutes: -1}", "must be greater than zero"},
		{"{min-minutes: null}", "must be a number"},
		{"{min-minutes: true}", "must be a number"},
		{"{min-minutes: [5]}", "must be a number"},
		{"{min-minutes: {value: 5}}", "must be a number"},
		{"{min-minutes: NaN}", "must be finite"},
		{"{min-minutes: +Inf}", "must be finite"},
		{"{max-minutes: NaN}", "must be finite"},
		{"{max-minutes: +Inf}", "must be finite"},
		{"{min-minutes: 1e999}", "must be a number"},
		{"{min-minutes: 30, max-minutes: 5}", "must not exceed"},
		{"{max-minutes: 5, min-minutes: 30}", "must not exceed"},
	} {
		t.Run(tc.mapping, func(t *testing.T) {
			_, err := ParseConfig([]byte("policy:\n  require-job-timeout: " + tc.mapping))
			if err == nil || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), "line:2,col:") {
				t.Fatalf("expected positioned %q error, got %v", tc.message, err)
			}
		})
	}
}

func TestJobTimeoutRangeAPI(t *testing.T) {
	for _, bounds := range [][2]float64{{0, 0}, {5, 0}, {0, 60}, {5, 60}, {5, 5}} {
		p, err := RequireJobTimeoutRange(bounds[0], bounds[1])
		if err != nil || !p.Enabled() {
			t.Fatalf("bounds %v: policy %v, error %v", bounds, p, err)
		}
		lower, hasMin := p.MinMinutes()
		upper, hasMax := p.MaxMinutes()
		if lower != bounds[0] || upper != bounds[1] || hasMin != (lower > 0) || hasMax != (upper > 0) {
			t.Fatalf("bounds %v: got (%v, %v), enabled (%v, %v)", bounds, lower, upper, hasMin, hasMax)
		}
	}
	for _, bounds := range [][2]float64{{-1, 5}, {5, -1}, {30, 5}, {math.NaN(), 5}, {5, math.Inf(1)}} {
		if p, err := RequireJobTimeoutRange(bounds[0], bounds[1]); err == nil || p != nil {
			t.Fatalf("accepted invalid bounds %v", bounds)
		}
	}
	for _, p := range []*JobTimeoutPolicy{nil, {}, {minMinutes: 5}, RequireJobTimeout(30)} {
		if lower, ok := p.MinMinutes(); lower != 0 || ok {
			t.Fatalf("unexpected minimum on %v", p)
		}
	}
	p, err := RequireJobTimeoutRange(5, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte("true"), p); err != nil {
		t.Fatal(err)
	}
	if lower, ok := p.MinMinutes(); lower != 0 || ok {
		t.Fatal("decoding a new value retained an old minimum")
	}
}

func TestConfigRequirePermissions(t *testing.T) {
	for _, tc := range []struct {
		value   string
		set     bool
		enabled bool
		scope   string
	}{
		{"null", false, false, ""},
		{"false", true, false, ""},
		{"true", true, true, "workflow"},
		{"{}", true, true, "workflow"},
		{"{scope: workflow}", true, true, "workflow"},
		{"{scope: job}", true, true, "job"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := ParseConfig([]byte("policy: {require-permissions: " + tc.value + "}"))
			if err != nil {
				t.Fatal(err)
			}
			p := cfg.RequiresPermissions()
			if (p != nil) != tc.set || p.Enabled() != tc.enabled || p.Scope() != tc.scope {
				t.Fatalf("unexpected policy %+v", p)
			}
		})
	}
	for _, tc := range []struct{ value, message string }{
		{"1", "must be a boolean or a mapping"},
		{"'true'", "must be a boolean or a mapping"},
		{"[]", "must be a boolean or a mapping"},
		{"{level: job}", "unknown key"},
		{"{scope: step}", `must be "workflow" or "job"`},
		{"{scope: null}", `must be "workflow" or "job"`},
		{"{scope: 1}", `must be "workflow" or "job"`},
		{"{scope: [job]}", `must be "workflow" or "job"`},
	} {
		t.Run(tc.value, func(t *testing.T) {
			_, err := ParseConfig([]byte("policy:\n  require-permissions: " + tc.value))
			if err == nil || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), "line:2,col:") {
				t.Fatalf("expected positioned %q error, got %v", tc.message, err)
			}
		})
	}
	for _, cfg := range []*Config{nil, {}, {Policy: Policy{}}} {
		if cfg.RequiresPermissions() != nil {
			t.Fatal("unset policy must remain nil")
		}
	}
	for _, scope := range []string{"workflow", "job"} {
		p, err := RequirePermissions(scope)
		if err != nil || !p.Enabled() || p.Scope() != scope {
			t.Fatalf("scope %q: policy %v, error %v", scope, p, err)
		}
	}
	if p, err := RequirePermissions("step"); err == nil || p != nil {
		t.Fatal("accepted an invalid scope")
	}
}
