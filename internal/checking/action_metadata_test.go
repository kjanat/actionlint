package checking

import (
	"context"
	"strings"
	"testing"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type metadataProvider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p metadataProvider) Lookup(ctx context.Context, r actions.Reference) (actions.Manifest, error) {
	return p(ctx, r)
}

func TestRequiredActionMetadataAndCoverage(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		cfg, err := ConfigurationRules().ParseOverlay("config", []byte("lint: {rules: {conventions: {missing-action-metadata: "+map[bool]string{false: "off", true: "on"}[enabled]+"}}}"))
		if err != nil {
			t.Fatal(err)
		}
		for _, missing := range []availability.Unavailability{availability.Offline(), availability.CacheMiss(), availability.LookupDisabled(), availability.Failed(), availability.NotFound()} {
			result, err := Check(t.Context(), Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{cfg}, RemoteActions: metadataProvider(func(context.Context, actions.Reference) (actions.Manifest, error) {
				return actions.UnavailableManifest(missing), nil
			})}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}"), Name: "workflow.yml"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Availability.Reason() != missing.Reason() {
				t.Fatalf("reason=%s coverage=%v", missing.Reason(), result.ActionMetadata)
			}
			count := 0
			for _, d := range result.Diagnostics {
				if d.Rule == "missing-action-metadata" {
					count++
					if !strings.Contains(d.Message, string(missing.Reason())) {
						t.Fatalf("missing reason: %s", d.Message)
					}
				}
			}
			want := 0
			if enabled && (missing.Reason() == availability.FailedReason || missing.Reason() == availability.NotFoundReason) {
				want = 1
			}
			if count != want {
				t.Fatalf("enabled=%v reason=%s findings=%v", enabled, missing.Reason(), result.Diagnostics)
			}
		}
	}
}
