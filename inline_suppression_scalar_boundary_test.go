package actionlint

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestInlineSuppressionCompactScalarBoundary(t *testing.T) {
	const header = " # actionlint:ignore inline-suppression,expression -- reviewed\n"
	const outside = "# actionlint:ignore-next-line unknown-rule -- malformed\n"
	for _, tc := range []struct{ name, source string }{
		{"compact literal", "- run: |" + header + "    body_marker\n  " + outside + "  uses: sibling\n"},
		{"compact folded", "- run: >-" + header + "    body_marker\n  " + outside + "  uses: sibling\n"},
		{"compact explicit", "- run: |2+" + header + "      body_marker\n    # actual scalar text\n  " + outside + "  uses: sibling\n"},
		{"compact one-space content", "- run: |1" + header + "   body_marker\n  " + outside + "  uses: sibling\n"},
		{"nested compact", "steps:\n  - run: |" + header + "      body_marker\n    " + outside + "    uses: sibling\n"},
		{"expanded mapping", "-\n  run: |" + header + "    body_marker\n  " + outside + "  uses: sibling\n"},
		{"separate tagged header", "- run: !!str\n    |" + header + "      body_marker\n  " + outside + "  uses: sibling\n"},
		{"dedented comment", "- run: |" + header + "      body_marker\n    " + outside + "  uses: sibling\n"},
		{"leading blank body", "- run: |" + header + "\n    body_marker\n\n  " + outside + "  uses: sibling\n"},
		{"empty scalar", "- run: |" + header + "  " + outside + "  uses: sibling\n"},
		{"empty explicit scalar", "- run: |2-" + header + "\n  " + outside + "  uses: sibling\n"},
		{"leading body comment", "- run: |" + header + "    # actual scalar text\n    body_marker\n  " + outside + "  uses: sibling\n"},
		{"anchored mapping", "- &step\n    run: |2" + header + "      body_marker\n    " + outside + "    uses: sibling\n"},
		{"tagged mapping", "- !!map\n    run: |2" + header + "      body_marker\n    " + outside + "    uses: sibling\n"},
		{"anchored sequence", "scripts: &scripts\n  - |2" + header + "    body_marker\n  " + outside + "  - uses: sibling\n"},
		{"separate sequence header", "scripts:\n  -\n    |2" + header + "    body_marker\n  " + outside + "  - uses: sibling\n"},
		{"anchored separate sequence header", "scripts: &scripts\n  -\n    |2" + header + "    body_marker\n  " + outside + "  - uses: sibling\n"},
		{"explicit mapping key", "- ? run\n  : |2" + header + "    body_marker\n  " + outside + "  uses: sibling\n"},
		{"anchored explicit mapping key", "- &step\n  ? run\n  : |2" + header + "    body_marker\n  " + outside + "  uses: sibling\n"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+ending, func(t *testing.T) {
				var bodyLine, bodyCommentLine, outsideLine, siblingLine int
				for i, line := range strings.Split(tc.source, "\n") {
					switch {
					case strings.Contains(line, "body_marker"):
						bodyLine = i + 1
					case strings.Contains(line, "actual scalar text"):
						bodyCommentLine = i + 1
					case strings.Contains(line, "unknown-rule"):
						outsideLine = i + 1
					case strings.Contains(line, "uses:"):
						siblingLine = i + 1
					}
				}
				source := []byte(strings.ReplaceAll(tc.source, "\n", ending))
				var document yaml.Node
				if err := yaml.Unmarshal(source, &document); err != nil {
					t.Fatalf("invalid YAML fixture: %v", err)
				}
				var findings []*Error
				if bodyLine != 0 {
					findings = append(findings, &Error{Line: bodyLine, Kind: "expression"})
				}
				if bodyCommentLine != 0 {
					findings = append(findings, &Error{Line: bodyCommentLine, Kind: "expression"})
				}
				sibling := &Error{Line: siblingLine, Kind: "expression"}
				findings = append(findings, sibling)
				got := filterInlineSuppressions(source, findings, nil)
				if len(got) != 2 || got[0] != sibling || got[1].Kind != "inline-suppression" || got[1].Line != outsideLine {
					t.Fatalf("block suppression crossed its scalar boundary: %+v", got)
				}
			})
		}
	}
}

func TestInlineSuppressionDedentedCommentBeforeScalarBody(t *testing.T) {
	for _, indicator := range []string{"|", "|2-", ">+"} {
		source := "- run: " + indicator + " # actionlint:ignore expression -- reviewed\n  # outside scalar\n    body_marker\n  uses: sibling\n"
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(source), &document); err == nil {
			t.Fatalf("dedented comment unexpectedly allowed before scalar body: %s", source)
		}
	}
}
