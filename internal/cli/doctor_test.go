package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestDoctorToolPresentation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := checkInvocation{Config: actionlint.ConfigSelection{Disabled: true}, ShellCheck: `"` + filepath.ToSlash(executable) + `"`}
	var text, data bytes.Buffer
	if err := writeDoctor(&text, request, false); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	column := -1
	for _, line := range strings.Split(text.String(), "\n")[1:] {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[label] = strings.TrimSpace(value)
		if values[label] != "" {
			start := strings.Index(line, values[label])
			if column != -1 && start != column {
				t.Fatalf("doctor values are not aligned:\n%s", &text)
			}
			column = start
		}
	}
	if filepath.Clean(values["shellcheck"]) != filepath.Clean(executable) || values["pyflakes"] != "disabled" {
		t.Fatalf("unexpected tool display:\n%s", &text)
	}
	if err := writeDoctor(&data, request, true); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Tools []doctorTool `json:"tools"`
	}
	if err := json.Unmarshal(data.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 2 || report.Tools[0].Status != "available" || report.Tools[0].Path == "" || report.Tools[1].Status != "disabled" {
		t.Fatalf("JSON lost tool status: %s", &data)
	}
}
