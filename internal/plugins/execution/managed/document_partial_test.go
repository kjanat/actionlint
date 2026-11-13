package managed

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

type partialGuest struct {
	fallbackGuest
	failAt, calls int
	grant         bool
	failure       error
}

func (guest *partialGuest) NegotiateTool(ctx context.Context, data, options json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	index := guest.calls
	guest.calls++
	if index == guest.failAt {
		if guest.grant {
			return &checkinputs.ToolNegotiation{Resources: []checkinputs.ToolResourceRequest{{Kind: checkinputs.ToolWorkspace}}}, nil
		}
		return nil, guest.failure
	}
	return guest.fallbackGuest.NegotiateTool(ctx, data, options)
}

func TestDocumentPluginRetainsOtherScriptsAfterFailure(t *testing.T) {
	t.Parallel()
	for _, failureAt := range []int{0, 1} {
		for _, grant := range []bool{false, true} {
			batch := documentBatchFixture(t, 3)
			nativeIndex := 1 - failureAt
			cfg, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {config: .shellcheckrc}}"))
			if err != nil {
				t.Fatal(err)
			}
			batch.Scripts[nativeIndex].Config = cfg
			failure := errors.New("negotiation failed")
			guest := &partialGuest{failAt: failureAt, grant: grant, failure: failure}
			nativeCalls := 0
			runner := documentRunner{
				documentRoute: shellcheckRoute, guest: guest, integrator: guest,
				context: tools.Context{Document: checkinputs.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}},
				native: func(*config.Config) (tools.Runner, error) {
					return fallbackRunner(func(_ context.Context, selected tools.Batch) ([]tools.Finding, error) {
						nativeCalls++
						if len(selected.Scripts) != 1 || selected.Scripts[0].Fact.Occurrence.ID != batch.Scripts[nativeIndex].Fact.Occurrence.ID || len(selected.Scopes) != 0 {
							t.Fatal("native fallback selection changed")
						}
						return []tools.Finding{{Script: 0, Message: "native"}}, nil
					}), nil
				},
			}
			findings, err := runner.Analyze(t.Context(), batch)
			if grant {
				if err == nil || !strings.Contains(err.Error(), "source access requires") {
					t.Fatalf("source failure lost: %v", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("negotiation failure lost: %v", err)
			}
			if nativeCalls != 1 || len(guest.inputs) != 1 || len(findings) != 2 || findings[0].Script != 2 || findings[1].Script != nativeIndex {
				t.Fatalf("siblings lost: native=%d plugin=%d findings=%+v", nativeCalls, len(guest.inputs), findings)
			}
		}
	}
}
