// Package resolution implements injected dependency resolvers and analyzer adapters.
package resolution

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"actionlint.kjanat.dev/v2/resolver"
)

// Process invokes a literal executable using the bounded JSON protocol. No shell
// interpolation or credential discovery occurs. Each session caches exact
// requests, including consumer kind and expected repository identity.
type Process struct {
	executable string
	arguments  []string
	timeout    time.Duration
	mu         sync.Mutex
	cache      map[resolver.Request]resolver.Result
	pending    map[resolver.Request]*pendingResolution
}

// defaultProcessTimeout bounds one provider run when NewProcess gets no timeout.
const defaultProcessTimeout = 30 * time.Second

// NewProcess returns a resolver that runs executable with a copy of arguments.
// A zero timeout means 30 seconds per provider run.
func NewProcess(executable string, arguments []string, timeout time.Duration) *Process {
	if timeout == 0 {
		timeout = defaultProcessTimeout
	}
	return &Process{executable: executable, arguments: slices.Clone(arguments), timeout: timeout}
}

type pendingResolution struct {
	done   chan struct{}
	result resolver.Result
	err    error
}

// boundedOutput collects provider stdout and fails the write that would take
// it past limit bytes.
type boundedOutput struct {
	data  []byte
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		return 0, fmt.Errorf("resolver response exceeds %d bytes", b.limit)
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func (p *Process) Resolve(ctx context.Context, request resolver.Request) (resolver.Result, error) {
	if err := ctx.Err(); err != nil {
		return resolver.Result{}, err
	}
	p.mu.Lock()
	cached, exists := p.cache[request]
	if exists {
		p.mu.Unlock()
		cached.Evidence = slices.Clone(cached.Evidence)
		return cached, nil
	}
	if call, exists := p.pending[request]; exists {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return resolver.Result{}, ctx.Err()
		case <-call.done:
			if err := ctx.Err(); err != nil {
				return resolver.Result{}, err
			}
			result := call.result
			result.Evidence = slices.Clone(result.Evidence)
			return result, call.err
		}
	}
	if p.pending == nil {
		p.pending = make(map[resolver.Request]*pendingResolution)
	}
	call := &pendingResolution{done: make(chan struct{})}
	p.pending[request] = call
	p.mu.Unlock()

	result, err := p.resolve(ctx, request)
	p.mu.Lock()
	call.result, call.err = result, err
	call.result.Evidence = slices.Clone(result.Evidence)
	if err == nil {
		if p.cache == nil {
			p.cache = make(map[resolver.Request]resolver.Result)
		}
		p.cache[request] = call.result
	}
	delete(p.pending, request)
	close(call.done)
	p.mu.Unlock()
	return result, err
}

func (p *Process) resolve(ctx context.Context, request resolver.Request) (resolver.Result, error) {
	if request.Version != 1 || (request.Kind != "action" && request.Kind != "workflow") {
		return resolver.Result{}, errors.New("unsupported resolution request")
	}
	data, err := jsonv2.Marshal(request)
	if err != nil {
		return resolver.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, p.executable, p.arguments...)
	command.WaitDelay = time.Second
	command.Stdin = bytes.NewReader(data)
	output := &boundedOutput{limit: 2 << 20}
	command.Stdout = output
	// Provider stderr can contain credentials or request internals. The structured
	// result carries public errors; an abnormal exit reports only process status.
	if err := command.Run(); err != nil {
		return resolver.Result{}, fmt.Errorf("resolver process: %w", err)
	}
	var result resolver.Result
	if err := jsonv2.Unmarshal(output.data, &result); err != nil {
		return result, fmt.Errorf("invalid resolver response: %w", err)
	}
	if err := validateResult(request, result); err != nil {
		return result, err
	}

	return result, nil
}

func (p *Process) NewSession() resolver.Resolver {
	return &Process{executable: p.executable, arguments: slices.Clone(p.arguments), timeout: p.timeout}
}

func validateResult(request resolver.Request, result resolver.Result) error {
	if result.Version != 1 {
		return fmt.Errorf("unsupported resolver response version %d", result.Version)
	}
	if result.Error != "" {
		return fmt.Errorf("resolution failed: %s", result.Error)
	}
	if result.RepositoryID == 0 || !strings.EqualFold(result.Repository, request.Repository) || request.ExpectedRepositoryID != 0 && result.RepositoryID != request.ExpectedRepositoryID || (result.Commit == "") == (result.Tree == "") || result.ObservedAt == "" || len(result.Evidence) == 0 {
		return errors.New("resolver returned incomplete or inconsistent provenance")
	}
	object := result.Commit
	if object == "" {
		object = result.Tree
	}
	if len(object) != 40 && len(object) != 64 {
		return errors.New("resolver returned invalid object ID")
	}
	for _, c := range object {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("resolver returned invalid object ID")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, result.ObservedAt); err != nil {
		return errors.New("resolver returned invalid observation time")
	}
	if bytes.Equal(bytes.TrimSpace(result.Evidence), []byte("null")) {
		return errors.New("resolver returned no evidence")
	}
	return nil
}
