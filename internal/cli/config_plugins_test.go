package cli

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestConfigInspectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, request := range []commandRequest{
		configShowRequest{}, configValidateRequest{}, doctorRequest{},
	} {
		if _, err := request.run(ctx, Command{Stdout: io.Discard}); !errors.Is(err, context.Canceled) {
			t.Errorf("%T ignored cancellation: %v", request, err)
		}
	}
}
