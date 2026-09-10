package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestCommandJSONHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help", "--", "--json"}, {"--help", "--stdin-filename", "--json"}, {"--help", "workflow.yml", "--json"}} {
		got := testRunCommand("", args...)
		if got.Status != 0 || got.Stdout != "" || !strings.Contains(got.Stderr, "Usage:") {
			t.Fatalf("literal data selected JSON help: %+v", got)
		}
	}
	for _, args := range [][]string{{"--help", "--json"}, {"--json", "-h"}, {"--output=json", "--help"}} {
		got := testRunCommand("", args...)
		var help commandDescription
		if err := json.Unmarshal([]byte(got.Stdout), &help); err != nil {
			t.Fatalf("%+v: %v", got, err)
		}
		if got.Status != 0 || got.Stderr != "" || help.Name != "actionlint" || len(help.Commands) != 6 {
			t.Fatalf("%+v", got)
		}
		for _, flag := range help.Flags {
			if flag.Description == "" || flag.Group == "" || flag.Name == "" {
				t.Errorf("incomplete flag: %+v", flag)
			}
			if flag.Name == "ignore-regex" && !flag.Repeatable {
				t.Error("ignore must be repeatable")
			}
		}
	}
	got := testRunCommand("", "--version", "--json")
	var info commandBuildInfo
	if err := json.Unmarshal([]byte(got.Stdout), &info); err != nil {
		t.Fatal(err)
	}
	if got.Status != 0 || got.Stderr != "" || info != commandBuild() {
		t.Fatalf("%+v", got)
	}
}

func TestManualNativeJSONContract(t *testing.T) {
	data, err := os.ReadFile("man/actionlint.1.md")
	if err != nil {
		t.Fatal(err)
	}
	result := testRunCommand(commandGoodWorkflow, "check", "--json", "-")
	if result.Status != 0 || !strings.Contains(string(data), strings.TrimSpace(result.Stdout)) {
		t.Fatalf("manual lacks clean JSON example %s", result.Stdout)
	}
}

func TestManualDocumentsEveryFlag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("man", "actionlint.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n# FLAGS\n")
	if !ok {
		t.Fatal("manual has no FLAGS section")
	}
	body, _, _ = strings.Cut(body, "\n# ")
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`\*\*(--?[^*]+)\*\*`).FindAllStringSubmatch(body, -1) {
		documented[m[1]] = true
	}
	app := newCommandApp(&Command{})
	app.root.Flags().VisitAll(func(f *pflag.Flag) {
		for _, name := range []string{"--" + f.Name, "-" + f.Shorthand} {
			if name == "-" {
				continue
			}
			if !documented[name] {
				t.Errorf("manual is missing %s", name)
			}
			delete(documented, name)
		}
	})
	for name := range documented {
		t.Errorf("manual documents unknown flag %s", name)
	}
}
