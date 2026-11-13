package config

import (
	"errors"
	"fmt"
	"testing"
)

func TestDecodeErrorPreservesSentinelIdentity(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("first line\nsecond line")
	cause := fmt.Errorf("configuration decode: %w", sentinel)
	err := decodeError{cause: cause}
	if err.Error() != "configuration decode: first line second line" {
		t.Fatalf("multiline diagnostic not normalized: %q", err.Error())
	}
	if !errors.Is(err, sentinel) || !errors.Is(err, cause) {
		t.Fatal("decoder cause identity lost")
	}
}
