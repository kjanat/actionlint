package repoconfig

import (
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestDefaultConfigReferenceExamples(t *testing.T) {
	// Enable the commented YAML settings to check that the reference stays valid
	// as the real configuration types and rule options evolve.
	setting := regexp.MustCompile(`^\s*(?:[a-z][a-z-]*:|[".].*:|- )`)
	var example strings.Builder
	for line := range strings.SplitSeq(defaultConfigTemplate, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(line[indent:], "# ") {
			line = line[:indent] + line[indent+2:]
		}
		if setting.MatchString(line) {
			example.WriteString(line)
			example.WriteString("\n")
		}
	}
	var config Config
	if err := yaml.Unmarshal([]byte(example.String()), &config); err != nil {
		t.Fatalf("invalid reference example: %v\n%s", err, example.String())
	}
}
