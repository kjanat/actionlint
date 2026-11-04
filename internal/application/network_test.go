package application

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/knowledge"
	"actionlint.kjanat.dev/v2/network"
)

func TestAnalysisSharesNetworkSession(t *testing.T) {
	const workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"
	for _, entry := range []string{"Check", "Analyze"} {
		for _, supplied := range []bool{false, true} {
			t.Run(entry+map[bool]string{false: "/created", true: "/supplied"}[supplied], func(t *testing.T) {
				ctx := t.Context()
				var expected *network.RequestTracker
				if supplied {
					ctx, expected = network.TrackRequests(ctx)
				}
				var observed *network.RequestTracker
				options := Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: metadataProvider(func(ctx context.Context, _ actions.Reference) (actions.Manifest, error) {
					observed = network.RequestTrackerFromContext(ctx)
					return actions.UnavailableManifest(knowledge.Offline()), nil
				})}
				var err error
				if entry == "Check" {
					_, err = Check(ctx, options, Input{Kind: Stdin, Reader: strings.NewReader(workflow), Name: "workflow.yml"})
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
