package checking

import (
	"time"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

// ExecutionEnvironment holds invocation-wide execution facts and trusted policy.
// Repository configuration supplies the source-specific service when preparing a document.
type ExecutionEnvironment struct {
	Service           services.Service
	Runners           runnerapi.Options
	EnforcedLifecycle []lifecycle.Policy
}

func (environment ExecutionEnvironment) captureDate(now func() time.Time) ExecutionEnvironment {
	if environment.Runners.AsOf.IsZero() {
		environment.Runners.AsOf = runnerapi.DateOf(now())
	}
	return environment
}
