package engine

import (
	"context"
	"io"
	"runtime"

	"actionlint.kjanat.dev/internal/analyzerhost"
)

// Historical regression fixtures retain their invocation spelling, but production
// entrypoints borrow resources from the application session.
func Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResult, error) {
	return analyze(ctx, request, request.LogWriter, request.LogLevel)
}
func analyze(ctx context.Context, request AnalysisRequest, log io.Writer, level LogLevel) (*AnalysisResult, error) {
	if log == nil {
		log = io.Discard
	}
	if request.Processes == nil {
		request.Processes = analyzerhost.NewProcessPool(ctx, runtime.NumCPU())
		defer request.Processes.Wait()
	}
	return run(ctx, request.prepared(), log, level)
}

func newConcurrentProcess(ctx context.Context, parallel int) *analyzerhost.ProcessPool {
	return analyzerhost.NewProcessPool(ctx, parallel)
}
