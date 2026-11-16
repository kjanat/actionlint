// Package execprotocol runs dependency resolvers through the process protocol.
package execprotocol

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

// Process invokes a literal executable using the bounded JSON protocol. No shell
// interpolation or credential discovery occurs. Each session caches exact
// requests, including consumer kind and expected repository identity.
type Process struct {
	command processCommand
	cached  *references.CachedResolver
}

// processCommand runs one provider process per resolution, without caching.
type processCommand struct {
	executable string
	arguments  []string
	timeout    time.Duration
}

// defaultProcessTimeout bounds one provider run when NewProcess gets no timeout.
const defaultProcessTimeout = 30 * time.Second

// NewProcess returns a resolver that runs executable with a copy of arguments.
// A zero timeout means 30 seconds per provider run.
func NewProcess(executable string, arguments []string, timeout time.Duration) *Process {
	if timeout == 0 {
		timeout = defaultProcessTimeout
	}
	return newProcess(processCommand{executable: executable, arguments: slices.Clone(arguments), timeout: timeout})
}

func newProcess(command processCommand) *Process {
	return &Process{command: command, cached: references.NewCachedResolver(command)}
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

func (p *Process) Resolve(ctx context.Context, request references.Request) (references.ResolutionRecord, error) {
	return p.cached.Resolve(ctx, request)
}

// NewSession returns a resolver for the same command with an empty cache.
func (p *Process) NewSession() references.Resolver {
	command := p.command
	command.arguments = slices.Clone(command.arguments)
	return newProcess(command)
}

func (c processCommand) Resolve(ctx context.Context, request references.Request) (references.ResolutionRecord, error) {
	if err := network.RequireOnline(ctx); err != nil {
		return references.ResolutionRecord{}, err
	}
	var receipt *accounting.RequestReport
	defer func() { accounting.RecordResolverCall(ctx, receipt, false) }()
	if request.Version != 1 || (request.Kind != "action" && request.Kind != "workflow") {
		return references.ResolutionRecord{}, errors.New("unsupported resolution request")
	}
	data, err := jsonv2.Marshal(request)
	if err != nil {
		return references.ResolutionRecord{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, c.executable, c.arguments...)
	command.WaitDelay = time.Second
	command.Stdin = bytes.NewReader(data)
	output := &boundedOutput{limit: 2 << 20}
	command.Stdout = output
	// Provider stderr can contain credentials or request internals. The structured
	// result carries public errors; an abnormal exit reports only process status.
	if err := command.Run(); err != nil {
		return references.ResolutionRecord{}, fmt.Errorf("resolver process: %w", err)
	}
	var result references.ResolutionRecord
	if err := jsonv2.Unmarshal(output.data, &result); err != nil {
		return result, fmt.Errorf("invalid resolver response: %w", err)
	}
	receipt = result.Network
	if err := references.ValidateResult(request, result); err != nil {
		return result, err
	}

	return result, nil
}
