package config_test

import (
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
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
	var decoded repoconfig.Config
	raw := checking.ConfigurationRules().DecodeNode(document.Content[0], &decoded)
	if raw == nil {
		t.Fatal("invalid boolean accepted by decoder")
	}
	_, err := checking.ConfigurationRules().Parse(input)
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
