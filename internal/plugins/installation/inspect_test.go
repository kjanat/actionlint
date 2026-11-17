package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

func TestPinSourceLeavesLocalAndPinnedSourcesUnchanged(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"plugin.wasm",
		"plugins/100%/plugin.wasm",
		`C:\plugins\100%\plugin.wasm`,
		"file:///plugins/plugin.wasm",
		"sha256://0123456789abcdef",
		"https://example.com/plugin.wasm#sha256=0123456789abcdef",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := PinSource(t.Context(), source, pluginacquisition.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got != source {
				t.Fatalf("PinSource(%q) = %q, want unchanged source", source, got)
			}
		})
	}
}

// An explicitly supplied private artifact reaches its endpoint. The registry
// counterpart, which must refuse that endpoint, is in pluginregistry.
func TestInspectGrantsExplicitPrivateArtifactAccess(t *testing.T) {
	t.Parallel()
	digest := sha256.Sum256([]byte{0, 97, 115, 109, 13, 0, 1, 0})
	source := "oci://127.0.0.1:8443/private/plugin@sha256:" + hex.EncodeToString(digest[:])
	dials := 0
	stopped := errors.New("test stopped the authorized connection")
	acquisition := pluginacquisition.Options{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, stopped
	}}}}
	if _, err := Inspect(t.Context(), source, acquisition); !errors.Is(err, stopped) || dials == 0 {
		t.Fatalf("explicit artifact did not reach the selected endpoint: error=%v dials=%d", err, dials)
	}
}
