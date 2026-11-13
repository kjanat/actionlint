package checking

import (
	"context"
	"strings"
	"testing"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestAnalysisSharesNetworkSession(t *testing.T) {
	t.Parallel()
	const workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"
	for _, entry := range []string{"Check", "Analyze"} {
		for _, supplied := range []bool{false, true} {
			t.Run(entry+map[bool]string{false: "/created", true: "/supplied"}[supplied], func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				var expected *accounting.RequestTracker
				if supplied {
					ctx, expected = accounting.TrackRequests(ctx)
				}
				var observed *accounting.RequestTracker
				options := Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: metadataProvider(func(ctx context.Context, _ actions.Reference) (actions.Manifest, error) {
					observed = accounting.RequestTrackerFromContext(ctx)
					return actions.UnavailableManifest(availability.Offline()), nil
				})}
				var err error
				if entry == "Check" {
					_, err = Check(ctx, options, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(workflow), Name: "workflow.yml"})
				} else {
					_, err = Analyze(ctx, options, []sourceUnit{{Path: "workflow.yml", Content: []byte(workflow)}})
				}
				if err != nil || observed == nil || supplied && observed != expected {
					t.Fatalf("acquisition session=%p supplied=%p error=%v", observed, expected, err)
				}
			})
		}
	}
}
