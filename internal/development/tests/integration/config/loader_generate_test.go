package config_test

import (
	"regexp"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestDefaultConfigReferenceExamples(t *testing.T) {
	// Enable the commented YAML settings to check that the reference stays valid
	// as the real configuration types and rule options evolve.
	setting := regexp.MustCompile(`^\s*(?:[a-z][a-z-]*:|[".].*:|- )`)
	var example strings.Builder
	for line := range strings.SplitSeq(string(repoconfig.DefaultDocument("2.0.0")), "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(line[indent:], "# ") {
			line = line[:indent] + line[indent+2:]
		}
		if setting.MatchString(line) {
			example.WriteString(line)
			example.WriteString("\n")
		}
	}
	var config repoconfig.Config
	if err := checking.ConfigurationRules().Unmarshal([]byte(example.String()), &config); err != nil {
		t.Fatalf("invalid reference example: %v\n%s", err, example.String())
	}
}
