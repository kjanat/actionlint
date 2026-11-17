package cli

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"github.com/spf13/pflag"
)

func (a *commandApp) runnerFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.RunnerTarget, "runner-target", "github", "Execution target for runner resolution")
	f.StringVar(&a.inv.Check.RunnerProvider, "runner-provider", "", "Select an authoritative runner provider by name")
	f.StringVar(&a.inv.Check.RunnerSnapshot, "runner-snapshot", "", "Read a runner evidence snapshot from `FILE`")
	f.StringVar(&a.inv.Check.RunnerScope, "runner-scope", "", "Organization/repository scope of runner evidence")
	f.StringVar(&a.inv.Check.RunnerService, "runner-service", "", "Workflow service: github.com, github-data-residency, ghes or unknown")
	f.StringVar(&a.inv.Check.RunnerServiceVersion, "runner-service-version", "", "GitHub Enterprise Server version, when known")
	f.StringVar(&a.inv.Check.RunnerAsOf, "runner-as-of", "", "Assess runner support on YYYY-MM-DD instead of today")
	f.StringVar(&a.inv.Check.AsOf, "as-of", "", "Assess lifecycle events on YYYY-MM-DD instead of today")
	for _, name := range []string{"runner-target", "runner-provider", "runner-snapshot", "runner-scope", "runner-service", "runner-service-version", "runner-as-of", "as-of"} {
		annotateFlag(f, name, "Rule selection")
	}
}

func (c checkInvocation) runners() (runnerapi.Options, error) {
	options := runnerapi.Options{Target: c.RunnerTarget, Provider: c.RunnerProvider, Scope: c.RunnerScope}
	if c.RunnerService != "" || c.RunnerServiceVersion != "" {
		service, err := services.ParseService(c.RunnerService, c.RunnerServiceVersion)
		if err != nil {
			return options, err
		}
		options.Service = &service
	}
	if c.AsOf != "" && c.RunnerAsOf != "" && c.AsOf != c.RunnerAsOf {
		return options, errors.New("--as-of and --runner-as-of must specify the same date")
	}
	asOf := c.AsOf
	if asOf == "" {
		asOf = c.RunnerAsOf
	}
	if asOf != "" {
		date, err := lifecycle.ParseDate(asOf)
		if err != nil {
			return options, err
		}
		options.AsOf = date
	}
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
	var snapshot struct {
		Content    jsontext.Value      `json:"content"`
		Scope      string              `json:"scope"`
		ObtainedAt string              `json:"obtained_at"`
		Freshness  runnerapi.Freshness `json:"freshness"`
		Complete   bool                `json:"complete"`
	}
	if err := jsonv2.Unmarshal(data, &snapshot, jsonv2.RejectUnknownMembers(true)); err != nil {
		return options, fmt.Errorf("runner snapshot: %w", err)
	}
	content := string(snapshot.Content)
	kind := snapshot.Content.Kind()
	if kind == jsontext.KindString {
		if err := jsonv2.Unmarshal(snapshot.Content, &content); err != nil {
			return options, fmt.Errorf("runner snapshot content: %w", err)
		}
	}
	if kind == jsontext.KindNull {
		content = ""
	}
	options.Snapshot = runnerapi.RunnerSnapshot{
		Content: content, Scope: snapshot.Scope, ObtainedAt: snapshot.ObtainedAt,
		Freshness: snapshot.Freshness, Complete: snapshot.Complete,
	}
	switch options.Snapshot.Freshness {
	case runnerapi.Fresh, runnerapi.Stale, runnerapi.Missing:
	default:
		return options, errors.New("runner snapshot freshness must be fresh, stale or missing")
	}
	return options, nil
}
