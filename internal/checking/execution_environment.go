package checking

import (
	"time"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

// ExecutionEnvironment holds invocation-wide execution facts and trusted policy.
// Repository configuration supplies the source-specific service when preparing a document.
type ExecutionEnvironment struct {
	Service           services.Service
	Runners           runner.Options
	EnforcedLifecycle []lifecycle.Policy
}

func (environment ExecutionEnvironment) captureDate(now func() time.Time) ExecutionEnvironment {
	if environment.Runners.AsOf.IsZero() {
		environment.Runners.AsOf = runner.DateOf(now())
	}
	return environment
}
