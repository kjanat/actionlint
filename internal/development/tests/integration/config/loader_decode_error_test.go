package config_test

import (
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

func TestParseDecodeErrorPreservesTypeAndSingleLineText(t *testing.T) {
	t.Parallel()
	input := []byte("lint: {enabled: wrong}\n")
	parsed, err := document.Parse("", input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded repoconfig.Config
	raw := checking.ConfigurationRules().DecodeValue(parsed.Value, &decoded)
	if raw == nil {
		t.Fatal("invalid boolean accepted by decoder")
	}
	_, err = checking.ConfigurationRules().Parse(input)
	if err == nil {
		t.Fatal("invalid boolean accepted")
	}
	if _, ok := errors.AsType[*binding.Error](err); !ok {
		t.Fatalf("decoder's typed failure lost: %T: %v", err, err)
	}
	failure, _ := errors.AsType[*binding.Error](err)
	original, _ := errors.AsType[*binding.Error](raw)
	if strings.Contains(err.Error(), "\n") || failure.Path != original.Path || failure.Cause.Error() != original.Cause.Error() {
		t.Fatalf("typed single-line failure changed: got %q; raw %q", err.Error(), raw.Error())
	}
}
