package configuration

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
)

func TestReportingDefaultsInheritedBeforePlugins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	child := filepath.Join(dir, "actionlint.yaml")
	if err := os.WriteFile(base, []byte("output: {formats: [github, pretty], log-level: debug, summary: true, network-stats: true}\nlint: {experimental: true, rules: {disable: [uninstalled-rule], plugins: {uninstalled-rule: warn}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("extends: [base.yaml]\noutput: {summary: false, quiet: false}\nlint: {experimental: false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := rules.ReadBeforePluginsContext(context.Background(), child, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config
	if !reflect.DeepEqual(cfg.Output.Formats, OutputFormats{"github", "pretty"}) || cfg.Output.LogLevel == nil || *cfg.Output.LogLevel != "debug" || cfg.Output.Summary == nil || *cfg.Output.Summary || cfg.Output.Quiet == nil || *cfg.Output.Quiet || cfg.Output.NetworkStats == nil || !*cfg.Output.NetworkStats || cfg.Lint.Experimental == nil || *cfg.Lint.Experimental {
		t.Fatalf("unexpected inherited configuration: %+v %+v", cfg.Output, cfg.Lint.Experimental)
	}
	if len(loaded.Provenance.files) != 2 {
		t.Fatalf("missing inheritance provenance: %+v", loaded.Provenance)
	}
	if _, err := rules.ReadContext(context.Background(), child, nil, nil); err == nil || !strings.Contains(err.Error(), "uninstalled-rule") {
		t.Fatalf("full binding accepted missing plugin rule: %v", err)
	}
}

// Digests describe the content the load read, whatever the files hold later.
func TestProvenanceDigestsLoadedContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	child := filepath.Join(dir, "actionlint.yaml")
	baseContent, childContent := "lint: {experimental: true}\n", "extends: [base.yaml]\n"
	if err := os.WriteFile(base, []byte(baseContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte(childContent), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := rules.ReadContext(context.Background(), child, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("lint: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{base: sha256hex.Digest([]byte(baseContent)), child: sha256hex.Digest([]byte(childContent))}
	if got := loaded.Provenance.Digests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("digests = %v, want %v", got, want)
	}
}

func TestReportingValidation(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		want   string
	}{
		{"output: {formats: [invalid]}", "unknown output format"},
		{"output: {log-level: verbose}", "invalid log level"},
		{"output: {summary: nope}", "summary"},
		{"output: {unexpected: true}", "unexpected"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			_, err := rules.ParseConfig([]byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
