package managed

import (
	"errors"
	"os"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
)

func TestDocumentPluginValidatesScopesWithoutScripts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	invalid, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {config: missing.shellcheckrc}}"))
	if err != nil {
		t.Fatal(err)
	}
	guest := &fallbackGuest{}
	process := hostprocess.NewProcessPool(t.Context(), 1)
	// The executable never runs: no script is checked.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	nativeCalls := 0
	r := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, native: func(selected *config.Config) (tools.Runner, error) {
		if selected != invalid {
			t.Fatal("fallback initialized without the declined scope configuration")
		}
		nativeCalls++
		registration, err := scripts.RegisterProvider(scripts.Provider{Manifest: shellcheckManifest, Programs: bundled.Program, Selection: scripts.Selection{Process: &processconfig.Options{Executable: &executable}}})
		if err != nil {
			return nil, err
		}
		return registration.Build(tools.Context{Processes: process})
	}}
	batch := tools.Batch{Scopes: []*tools.Scope{{Config: invalid, AnalysisDir: root, Workspace: root}}}
	findings, err := r.Analyze(t.Context(), batch)
	if err == nil || !strings.Contains(err.Error(), "missing.shellcheckrc") || len(findings) != 0 || nativeCalls != 1 || len(guest.inputs) != 0 {
		t.Fatalf("scope configuration skipped: findings=%v native=%d inputs=%d err=%v", findings, nativeCalls, len(guest.inputs), err)
	}
}

func TestDocumentPluginScopesOnlyNegotiationAndDisable(t *testing.T) {
	t.Parallel()
	disabled, err := (config.RuleSet{}).Parse([]byte("tools: {shellcheck: {enabled: false}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*config.Config{nil, disabled} {
		failure := errors.New("configuration rejected")
		guest := &partialGuest{failAt: 0, failure: failure}
		r := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, native: func(*config.Config) (tools.Runner, error) {
			t.Fatal("failed or disabled negotiation reached native fallback")
			return nil, nil
		}}
		_, err := r.Analyze(t.Context(), tools.Batch{Scopes: []*tools.Scope{{Config: cfg}}})
		if cfg == nil {
			if !errors.Is(err, failure) || guest.calls != 1 {
				t.Fatalf("negotiation failure lost: %v calls=%d", err, guest.calls)
			}
		} else if err != nil || guest.calls != 0 {
			t.Fatalf("disabled scope negotiated: %v calls=%d", err, guest.calls)
		}
	}
}

func TestDocumentPluginRetainsScriptsBesideScopeFailure(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 1)
	batch.Scopes = []*tools.Scope{{}}
	failure := errors.New("empty scope failed")
	guest := &partialGuest{failAt: 1, failure: failure}
	r := documentRunner{
		documentRoute: shellcheckRoute, guest: guest, integrator: guest,
		context: tools.Context{Document: checkinputs.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml"}},
	}
	findings, err := r.Analyze(t.Context(), batch)
	if !errors.Is(err, failure) || len(findings) != 1 || findings[0].Script != 0 {
		t.Fatalf("script or scope failure lost: %v %v", findings, err)
	}
}
