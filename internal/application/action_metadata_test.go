package application

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/knowledge"
)

type metadataProvider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p metadataProvider) Lookup(ctx context.Context, r actions.Reference) (actions.Manifest, error) {
	return p(ctx, r)
}

func TestRequiredActionMetadataAndCoverage(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg, err := config.ParseOverlay("config", []byte("policy: {require-action-metadata: "+map[bool]string{false: "false", true: "true"}[enabled]+"}"))
		if err != nil {
			t.Fatal(err)
		}
		result, err := Check(t.Context(), Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{cfg}, RemoteActions: metadataProvider(func(context.Context, actions.Reference) (actions.Manifest, error) {
			return actions.UnavailableManifest(knowledge.Offline()), nil
		})}, Input{Kind: Stdin, Reader: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}"), Name: "workflow.yml"})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Availability.Reason() != knowledge.OfflineReason {
			t.Fatalf("coverage=%v", result.ActionMetadata)
		}
		count := 0
		for _, d := range result.Diagnostics {
			if d.Rule == "missing-action-metadata" {
				count++
				if !strings.Contains(d.Message, "offline") {
					t.Fatalf("missing reason: %s", d.Message)
				}
			}
		}
		if enabled && count != 1 || !enabled && count != 0 {
			t.Fatalf("enabled=%v findings=%v", enabled, result.Diagnostics)
		}
	}
}
