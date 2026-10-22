package cli

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/runner"
	"github.com/spf13/pflag"
)

func (a *commandApp) runnerFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.RunnerTarget, "runner-target", "github", "Execution target for runner resolution")
	f.StringVar(&a.inv.Check.RunnerProvider, "runner-provider", "", "Select an authoritative runner provider by name")
	f.StringVar(&a.inv.Check.RunnerSnapshot, "runner-snapshot", "", "Read a runner evidence snapshot from `FILE`")
	f.StringVar(&a.inv.Check.RunnerScope, "runner-scope", "", "Organization/repository scope of runner evidence")
	for _, name := range []string{"runner-target", "runner-provider", "runner-snapshot", "runner-scope"} {
		annotateFlag(f, name, "Rule selection")
	}
}

func (c checkInvocation) runners() (runner.Options, error) {
	options := runner.Options{Target: c.RunnerTarget, Provider: c.RunnerProvider, Scope: c.RunnerScope}
	if c.RunnerSnapshot == "" {
		return options, nil
	}
	f, err := os.Open(c.RunnerSnapshot)
	if err != nil {
		return options, err
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		return options, err
	}
	if len(data) > limit {
		return options, fmt.Errorf("runner snapshot exceeds %d bytes", limit)
	}
	if err := jsonv2.Unmarshal(data, &options.Snapshot, jsonv2.RejectUnknownMembers(true)); err != nil {
		return options, fmt.Errorf("runner snapshot: %w", err)
	}
	switch options.Snapshot.Freshness {
	case runner.Fresh, runner.Stale, runner.Missing:
	default:
		return options, errors.New("runner snapshot freshness must be fresh, stale or missing")
	}
	return options, nil
}
