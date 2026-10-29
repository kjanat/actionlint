package localapp

import (
	"encoding/json"
	"testing"
)

func TestDeclaresLanguageReadsStaticMetadata(t *testing.T) {
	ruff := json.RawMessage(`{"name":"Ruff","languages":["python"]}`)
	shell := json.RawMessage(`{"name":"ShellCheck","languages":["shell"]}`)
	for _, test := range []struct {
		name     string
		metadata json.RawMessage
		disabled []string
		want     bool
	}{
		{"python disabled", ruff, []string{"python"}, true},
		{"shell disabled", ruff, []string{"shell"}, false},
		{"both disabled", shell, []string{"python", "shell"}, true},
		{"nothing disabled", ruff, nil, false},
		{"no metadata", nil, []string{"python"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := declaresLanguage(test.metadata, test.disabled)
			if err != nil || got != test.want {
				t.Fatalf("declaresLanguage = %t, %v; want %t", got, err, test.want)
			}
		})
	}
	if _, err := declaresLanguage(json.RawMessage(`{`), []string{"python"}); err == nil {
		t.Fatal("malformed metadata was accepted")
	}
}
