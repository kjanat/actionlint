package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kjanat/go-yaml/v4"
)

func TestParseDecodeErrorPreservesTypeAndSingleLineText(t *testing.T) {
	input := []byte("lint: {enabled: wrong}\n")
	var document yaml.Node
	if err := yaml.Unmarshal(input, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Content) != 1 {
		t.Fatal("missing configuration root")
	}
	var decoded Config
	raw := DecodeNode(document.Content[0], &decoded)
	if raw == nil {
		t.Fatal("invalid boolean accepted by decoder")
	}
	_, err := Parse(input)
	if err == nil {
		t.Fatal("invalid boolean accepted")
	}
	if _, ok := errors.AsType[*yaml.LoadErrors](err); !ok {
		t.Fatalf("decoder's typed failure lost: %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "\n") || err.Error() != strings.ReplaceAll(raw.Error(), "\n", " ") {
		t.Fatalf("single-line diagnostic changed: got %q; raw %q", err.Error(), raw.Error())
	}
}

func TestDecodeErrorPreservesSentinelIdentity(t *testing.T) {
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
