package shellcheck

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
)

func TestInputOptionsRejectUnavailableSourceAccess(t *testing.T) {
	for _, settings := range []string{
		"'shellcheck.rc'",
		"{source-path: [SCRIPTDIR]}",
		"{external-sources: true}",
	} {
		cfg, err := config.Parse([]byte("tools: {shellcheck: {config: " + settings + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := prepareInput(tools.Script{Config: cfg, Text: "echo input"}); err == nil {
			t.Fatalf("input route accepted unavailable source access: %s", settings)
		}
	}
}

func TestInputOptionsKeepLanguageAndDirectiveCoordinates(t *testing.T) {
	cfg, err := config.Parse([]byte("tools: {shellcheck: {config: {shell: dash, disable: [SC2086]}}}"))
	if err != nil {
		t.Fatal(err)
	}
	site := &analyzer.ScriptInput{Language: "sh"}
	source := "#!/bin/sh\n# shellcheck shell=ksh\necho $input\n"
	prepared, err := prepareInput(tools.Script{Config: cfg, Input: site, Text: source, Execution: tools.Execution{Shell: "bash --noprofile --norc -e -o pipefail {0}"}})
	if err != nil {
		t.Fatal(err)
	}
	var options struct {
		Shell string `json:"shell"`
	}
	if err := json.Unmarshal(prepared.Config, &options); err != nil {
		t.Fatal(err)
	}
	if options.Shell != "ksh" || !strings.HasPrefix(prepared.Source, "#!/bin/sh\n# shellcheck disable=SC2086\n") || strings.Contains(prepared.Source, "set -e") {
		t.Fatalf("input acquired runner semantics or lost directives: %+v", prepared)
	}
	script := func(line int) tools.PreparedLine { return tools.PreparedLine{Line: line, Origin: tools.LineScript} }
	if prepared.LineMap == nil || prepared.LineMap(1) != script(1) || prepared.LineMap(2) != (tools.PreparedLine{Origin: tools.LineDirective}) || prepared.LineMap(3) != script(2) || prepared.LineMap(4) != script(3) {
		t.Fatal("directive prefix changed source line ownership")
	}
	plain, err := prepareInput(tools.Script{Input: site, Text: "echo input", Execution: tools.Execution{Shell: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plain.Config, &options); err != nil {
		t.Fatal(err)
	}
	if options.Shell != "sh" || plain.Source != "echo input" || plain.LineMap != nil {
		t.Fatalf("input inherited runner shell or changed its unconfigured source: %+v", plain)
	}
}
