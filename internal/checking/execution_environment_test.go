package checking

import (
	"testing"
	"time"
)

func TestExecutionEnvironmentCapturesOneUTCDate(t *testing.T) {
	t.Parallel()
	reads := 0
	now := func() time.Time {
		reads++
		return time.Date(2026, time.November, 15, 0, 30, 0, 0, time.FixedZone("Amsterdam", 3600))
	}
	options := Options{ExecutionEnvironment: (ExecutionEnvironment{}).captureDate(now)}
	request := analysisRequest{ExecutionEnvironment: options.captureDate(now)}
	session := analysisSession{environment: request.captureDate(now)}
	if reads != 1 {
		t.Fatalf("read the assessment clock %d times, want once", reads)
	}
	if got := session.environment.Runners.AsOf.String(); got != "2026-11-14" {
		t.Fatalf("assessment date is %q, want the captured UTC date 2026-11-14", got)
	}
}
