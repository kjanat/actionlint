//go:build conformance

package actionlint

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type upstreamBlock struct{ Name, Body string }

func TestUpstreamConformanceControls(t *testing.T) {
	for _, c := range []upstreamCase{
		{ID: "parser accepts runtime coercion", Kind: "expression", Input: "1 == false"},
		{ID: "parser does not evaluate format", Kind: "expression", Input: "format('{2}', 'only one argument')"},
		{ID: "unknown context", Kind: "expression", Input: "secrets.TOKEN", Reject: true},
		{ID: "unknown function", Kind: "expression", Input: "unknown('value')", Reject: true},
		{ID: "function arity", Kind: "expression", Input: "contains('value')", Reject: true},
		{ID: "case argument parity", Kind: "expression", Input: "case(true, 'first', false, 'second')", Reject: true},
		{ID: "unterminated expression", Kind: "expression", Input: "inputs[", Reject: true},
		{ID: "expression terminator is not end of input", Kind: "expression", Input: "true }} ignored", Reject: true},
		{ID: "quoted expression terminator", Kind: "expression", Input: "'text }} still quoted'"},
		{ID: "invalid action runtime", Kind: "action", Input: "name: Test\ndescription: Test\nruns: {using: nonexistent, main: main.js}", Reject: true},
		{ID: "invalid workflow key", Kind: "workflow", Input: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    stepz: []", Reject: true},
	} {
		t.Run(c.ID, func(t *testing.T) {
			got := checkUpstreamCase(t, c)
			if c.Reject != (len(got) > 0) {
				t.Fatalf("want reject=%t, got %q", c.Reject, got)
			}
		})
	}
}

func upstreamBlocks(t *testing.T, input, pattern string, count int) []upstreamBlock {
	t.Helper()
	matches := regexp.MustCompile(pattern).FindAllStringSubmatchIndex(input, -1)
	if len(matches) != count {
		t.Fatalf("upstream test structure changed: expected %d blocks, found %d", count, len(matches))
	}
	blocks := make([]upstreamBlock, 0, len(matches))
	for i, match := range matches {
		end := len(input)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		blocks = append(blocks, upstreamBlock{input[match[2]:match[3]], input[match[1]:end]})
	}
	return blocks
}

func loadLanguageServiceCases(t *testing.T, root fs.FS) []upstreamCase {
	t.Helper()
	var cases []upstreamCase
	for _, suite := range []struct {
		File  string
		Count int
	}{
		{"validate.service-container-command.test.ts", 5},
		{"validate.yaml-anchors.test.ts", 8},
	} {
		file := "languageservice/src/" + suite.File
		input := readUpstream(t, root, file)
		blocks := upstreamBlocks(t, input, `(?m)^  it\("([^"]+)", async \(\) => \{`, suite.Count)
		for _, block := range blocks {
			templates := regexp.MustCompile("(?s)`([^`]+)`").FindAllStringSubmatch(block.Body, -1)
			if len(templates) != 1 || strings.ContainsAny(templates[0][1], "\\") || strings.Contains(templates[0][1], "${") {
				t.Fatalf("%s/%s: expected one literal template without interpolation or escapes", file, block.Name)
			}
			c := upstreamCase{ID: file + "/" + block.Name, Kind: "workflow", Input: templates[0][1]}
			c.Reject = strings.Contains(block.Body, ".toBeGreaterThan(0)")
			if !c.Reject && !strings.Contains(block.Body, ".toEqual([])") {
				if !strings.Contains(block.Body, "expect(result).toBeDefined()") {
					t.Fatalf("%s: unrecognized upstream assertion", c.ID)
				}
				c.CompletionOnly = true
			}
			for _, filter := range regexp.MustCompile(`d.message.includes\("([^"]+)"\)`).FindAllStringSubmatch(block.Body, -1) {
				c.Filter = append(c.Filter, filter[1])
			}
			cases = append(cases, c)
		}
	}
	return cases
}

func loadRunnerExpressions(t *testing.T, root fs.FS) []upstreamCase {
	t.Helper()
	const file = "src/Test/L0/Sdk/ExpressionParserL0.cs"
	input := readUpstream(t, root, file)
	blocks := upstreamBlocks(t, input, `public void (CreateTree_\w+)\(\)`, 4)
	var cases []upstreamCase
	for _, block := range blocks {
		calls := regexp.MustCompile(`parser.CreateTree\(("[^"\\]*"), null, namedValues, null\)`).FindAllStringSubmatch(block.Body, -1)
		names := regexp.MustCompile(`new NamedValueInfo<ContextValueNode>\("([^"\\]+)"\)`).FindAllStringSubmatch(block.Body, -1)
		if len(calls) != 1 || len(names) == 0 {
			t.Fatalf("%s/%s: unsupported upstream expression fixture", file, block.Name)
		}
		expr, err := strconv.Unquote(calls[0][1])
		if err != nil {
			t.Fatal(err)
		}
		c := upstreamCase{ID: file + "/" + block.Name, Input: expr, Kind: "expression", Contexts: map[string]json.RawMessage{}}
		c.Reject = strings.Contains(block.Body, "Assert.Throws<ParseException>")
		if !c.Reject && !strings.Contains(block.Body, "Assert.NotNull(node)") {
			t.Fatalf("%s: unrecognized upstream assertion", c.ID)
		}
		for _, name := range names {
			c.Contexts[name[1]] = json.RawMessage(`{}`)
		}
		cases = append(cases, c)
	}
	return cases
}
