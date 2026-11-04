package analyzerhost

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost/executor"
)

// PreparedRegistration retains private copies of validated registration metadata.
// Its zero value cannot execute. Analyzer implementations remain responsible for
// their own panics and concurrent access to their internal state.
type PreparedRegistration = executor.PreparedRegistration

// Run isolates each analyzer's input and joins all started work.
// Findings and errors follow registration order.
func Run(ctx context.Context, input analyzer.Input, registrations []analyzer.Registration) ([]diagnostic.Diagnostic, error) {
	return executor.Run(ctx, input, registrations)
}

// Prepare validates a registration once for an exact set of available input kinds.
func Prepare(registration analyzer.Registration, available []analyzer.InputKind) (PreparedRegistration, error) {
	return executor.Prepare(registration, available)
}

// PrepareForwarding admits a transparent adapter's implementation through the
// same executor while retaining its runtime error text.
func PrepareForwarding(registration analyzer.Registration, available []analyzer.InputKind) (PreparedRegistration, error) {
	return executor.PrepareForwarding(registration, available)
}

// RunOne isolates one invocation of a prepared registration, retaining partial
// findings and cancellation. Input kinds must match the set validated by Prepare.
func RunOne(ctx context.Context, input analyzer.Input, registration PreparedRegistration) ([]diagnostic.Diagnostic, error) {
	return executor.RunOne(ctx, input, registration)
}
