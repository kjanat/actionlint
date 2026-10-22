package editor

import (
	"slices"
	"strings"
	"testing"
)

func TestEventCompletions(t *testing.T) {
	for _, tc := range []struct{ name, trigger, value, want, absent string }{
		{"PR fields", "pull_request", "${{ github.event.pull_request.ti| }}", "title", "commits"},
		{"push fields", "push", "${{ github.event.co| }}", "commits", "pull_request"},
		{"unfinished", "pull_request", "${{ github.event.pull_request.ti|", "title", ""},
		{"brackets", "pull_request", "${{ github.event['pull_request']['ti|'] }}", "title", ""},
		{"arrays", "push", "${{ github.event.commits[0].mes| }}", "message", ""},
		{"filters", "push", "${{ github.event.commits.*.mes| }}", "message", ""},
		{"multiple triggers", "[push, pull_request]", "${{ github.event.pull| }}", "pull_request", ""},
		{"nested expression", "pull_request", "${{ contains(github.event.pull_request.ti|, 'x') }}", "title", ""},
		{"root", "push", "${{ gi| }}", "github", ""},
		{"inside string", "pull_request", "${{ 'github.event.pull_request.ti|' }}", "", "title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := "on: " + tc.trigger + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '" + tc.value + "'\n"
			offset := strings.Index(input, "|")
			input = strings.Replace(input, "|", "", 1)
			items := Complete(input, offset)
			labels := []string{}
			for _, item := range items {
				labels = append(labels, item.Label)
				if item.Start > offset || item.End < offset {
					t.Fatalf("invalid edit: %+v", item)
				}
			}
			if tc.want != "" && !slices.Contains(labels, tc.want) {
				t.Fatalf("missing %s: %v", tc.want, labels)
			}
			if tc.absent != "" && slices.Contains(labels, tc.absent) {
				t.Fatalf("unexpected %s: %v", tc.absent, labels)
			}
		})
	}
}

func TestCompletionReplacesWholeToken(t *testing.T) {
	content := "on: pull_request\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ github.event.pull_request.titel }}'\n"
	offset := strings.Index(content, "titel") + 2
	items := Complete(content, offset)
	for _, item := range items {
		if item.Label == "title" {
			if got := content[item.Start:item.End]; got != "titel" {
				t.Fatalf("replacement covers %q", got)
			}
			return
		}
	}
	t.Fatal("no title completion")
}

func TestCompletionAvailability(t *testing.T) {
	for _, content := range []string{
		"on: push\njobs:\n  test:\n    if: ${{ sec| }}\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n",
		"on: push\n# ${{ github.ev| }}\n",
	} {
		offset := strings.Index(content, "|")
		content = strings.Replace(content, "|", "", 1)
		if items := Complete(content, offset); len(items) != 0 {
			t.Fatalf("unexpected completions: %+v", items)
		}
	}
}
