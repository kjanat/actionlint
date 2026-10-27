package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/source/capture"
)

type initializationJoinFunc func(context.Context) error

func (f initializationJoinFunc) Join(ctx context.Context) error { return f(ctx) }

func TestSourceCaptureInitializationJoinCancelsLifetimeAndKeepsCallerValues(t *testing.T) {
	type contextKey struct{}
	parent, cancelParent := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "parent value"))
	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	cancelParent()
	cause := errors.New("initialization failed")
	called := false
	err := joinSourceCaptureInitialization(parent, cause, initializationJoinFunc(func(cleanup context.Context) error {
		called = true
		if !errors.Is(lifetime.Err(), context.Canceled) {
			t.Error("cleanup began before lifetime cancellation")
		}
		if cleanup.Err() != nil || cleanup.Value(contextKey{}) != "parent value" {
			t.Errorf("cleanup inherited cancellation or lost parent values: %v", cleanup.Err())
		}
		deadline, ok := cleanup.Deadline()
		if remaining := time.Until(deadline); !ok || remaining < time.Second || remaining > sourceCaptureJoinTimeout {
			t.Errorf("default initialization join budget = %v, has deadline=%t", remaining, ok)
		}
		return nil
	}), cancelLifetime, 0)
	if !called || !errors.Is(err, cause) {
		t.Fatalf("successful join lost initialization failure: called=%t error=%v", called, err)
	}
}

func TestSourceCaptureInitializationJoinReturnsWithinBudgetAndPreservesErrors(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())
	cancelParent()
	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	cause, joinFailure := errors.New("initialization failed"), errors.New("metadata still pending")
	returned := make(chan error, 1)
	go func() {
		returned <- joinSourceCaptureInitialization(parent, cause, initializationJoinFunc(func(cleanup context.Context) error {
			if !errors.Is(lifetime.Err(), context.Canceled) {
				t.Error("cleanup began before lifetime cancellation")
			}
			<-cleanup.Done()
			return errors.Join(joinFailure, cleanup.Err())
		}), cancelLifetime, 5*time.Millisecond)
	}()
	select {
	case err := <-returned:
		if !errors.Is(err, cause) || !errors.Is(err, joinFailure) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("initialization cleanup lost original/join errors: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initialization cleanup exceeded its bounded budget")
	}
}

func TestAnalysisCaptureInitializationFailureReturnsNoResult(t *testing.T) {
	var reads atomic.Int32
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir:     t.TempDir(),
		SourceOverlays: []SourceOverlay{{Path: "bad\x00.yml", Content: []byte("name: malformed")}},
		ReadFile: func(string) ([]byte, error) {
			reads.Add(1)
			return nil, nil
		},
	})
	initialization, ok := errors.AsType[*capture.InitError](err)
	if result != nil || !ok || initialization.Pending() || initialization.Code() != "capture-failed" || initialization.Stage() != "capture" || reads.Load() != 0 {
		t.Fatalf("initialization failure published or read: result=%v error=%v reads=%d", result, err, reads.Load())
	}
}

// blockedAnalysisInitError models the constructor's error lifecycle without
// exposing a resolver override in capture. Real InitError behavior is exercised
// by capture.TestNewCancelledMetadataWaitRetainsJoinOwnership.
type blockedAnalysisInitError struct {
	cause  error
	joined atomic.Bool
	join   func(context.Context) error
}

func (e *blockedAnalysisInitError) Error() string { return e.cause.Error() }
func (e *blockedAnalysisInitError) Unwrap() error { return e.cause }
func (e *blockedAnalysisInitError) Pending() bool { return !e.joined.Load() }
func (e *blockedAnalysisInitError) Join(ctx context.Context) error {
	err := e.join(ctx)
	if err == nil {
		e.joined.Store(true)
	}
	return err
}

func TestAnalysisBlockedCaptureConstructorCancelsJoinsAndReturnsNoResult(t *testing.T) {
	for _, complete := range []bool{true, false} {
		name := "join timeout"
		if complete {
			name = "joined"
		}
		t.Run(name, func(t *testing.T) {
			type contextKey struct{}
			parent, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "caller value"))
			pool := analyzerhost.NewProcessPool(parent, 1)
			t.Cleanup(pool.Wait)
			started, joinStarted, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			signalJoin := sync.OnceFunc(func() { close(joinStarted) })
			failure := &blockedAnalysisInitError{cause: context.Canceled}
			constructorErr := fmt.Errorf("capture constructor: %w", failure)
			lifetimeError := parent.Err
			var joinBudget time.Duration
			var lifetimeCancelled, cleanupUncancelled, valuesPreserved bool
			joinFailure := errors.New("metadata join timed out")
			failure.join = func(cleanup context.Context) error {
				lifetimeCancelled = errors.Is(lifetimeError(), context.Canceled)
				cleanupUncancelled = cleanup.Err() == nil
				valuesPreserved = cleanup.Value(contextKey{}) == "caller value"
				deadline, ok := cleanup.Deadline()
				if ok {
					joinBudget = time.Until(deadline)
				}
				signalJoin()
				select {
				case <-release:
					return nil
				case <-cleanup.Done():
					return errors.Join(joinFailure, cleanup.Err())
				}
			}
			t.Cleanup(func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("application did not exit after releasing initialization")
					return
				}
				cleanup, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				if err := failure.Join(cleanup); err != nil {
					t.Errorf("test initialization cleanup: %v", err)
				}
			})
			budget := 5 * time.Millisecond
			if complete {
				budget = time.Second
			}
			var reads atomic.Int32
			request := analysisRequest{
				Processes: pool, WorkingDir: t.TempDir(), captureJoinTimeout: budget,
				SourceOverlays: []SourceOverlay{{Path: "first.yml"}, {Path: "later.yml"}},
				ReadFile:       func(string) ([]byte, error) { reads.Add(1); return nil, nil },
			}
			type outcome struct {
				result *analysisResult
				err    error
			}
			returned := make(chan outcome, 1)
			go func() {
				defer close(finished)
				result, err := runWithSourceCaptureConstructor(parent, request, io.Discard, LogLevelNone,
					func(ctx context.Context, _ []capture.Overlay, _ capture.Reader) (*capture.Store, error) {
						lifetimeError = ctx.Err
						close(started)
						<-ctx.Done()
						return nil, constructorErr
					})
				returned <- outcome{result, err}
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("application did not start capture constructor")
			}
			cancel()
			select {
			case <-joinStarted:
			case <-time.After(time.Second):
				t.Fatal("application did not join pending initialization")
			}
			if complete {
				unblock()
			}
			select {
			case result := <-returned:
				underlying, ok := errors.AsType[*blockedAnalysisInitError](result.err)
				if result.result != nil || !ok || underlying != failure || !errors.Is(result.err, constructorErr) || !errors.Is(result.err, context.Canceled) || reads.Load() != 0 {
					t.Fatalf("pending initialization published or lost cause: result=%v error=%v reads=%d", result.result, result.err, reads.Load())
				}
				if !lifetimeCancelled || !cleanupUncancelled || !valuesPreserved || joinBudget <= 0 || joinBudget > budget {
					t.Fatalf("application join context: lifetime-cancelled=%t cleanup-uncancelled=%t values=%t budget=%v", lifetimeCancelled, cleanupUncancelled, valuesPreserved, joinBudget)
				}
				if complete {
					if failure.Pending() || errors.Is(result.err, joinFailure) {
						t.Fatalf("successful initialization join: pending=%t error=%v", failure.Pending(), result.err)
					}
				} else if !failure.Pending() || !errors.Is(result.err, joinFailure) || !errors.Is(result.err, context.DeadlineExceeded) {
					t.Fatalf("bounded application join lost pending work/error: pending=%t error=%v", failure.Pending(), result.err)
				}
			case <-time.After(time.Second):
				t.Fatal("application waited beyond initialization join budget")
			}
		})
	}
}
