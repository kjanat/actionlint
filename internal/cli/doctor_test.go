package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestDoctorToolPresentation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := doctorRequest{Config: repoconfig.Selection{Disabled: true}, ResolverCommand: executable, Hyperlinks: "never"}
	var text, data bytes.Buffer
	if err := writeDoctor(t.Context(), &text, request); err != nil {
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
	if filepath.Clean(values["resolver"]) != filepath.Clean(executable) {
		t.Fatalf("unexpected tool display:\n%s", &text)
	}
	request.JSON = true
	if err := writeDoctor(t.Context(), &data, request); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Tools []doctorTool `json:"tools"`
	}
	if err := json.Unmarshal(data.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Name != "resolver" || report.Tools[0].Status != "available" || report.Tools[0].Path == "" {
		t.Fatalf("JSON lost tool status: %s", &data)
	}
}

func TestDoctorResolverSelection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, command, status string
	}{
		{"available", executable, "available"},
		{"missing", filepath.Join(t.TempDir(), "missing-resolver"), "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			command := Command{Stdout: &out, Stderr: &stderr}
			args := []string{"actionlint", "doctor", "--no-config", "--json", "--resolve-with", tc.command, "--resolver-arg=argument with spaces", "--resolver-arg=--literal"}
			if status := command.MainContext(t.Context(), args); status != 0 {
				t.Fatalf("doctor failed: exit=%d stderr=%s", status, &stderr)
			}
			var report struct {
				Tools []doctorTool `json:"tools"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(report.Tools, func(tool doctorTool) bool { return tool.Name == "resolver" })
			if index < 0 {
				t.Fatalf("resolver omitted: %s", &out)
			}
			tool := report.Tools[index]
			if tool.Command != tc.command || tool.Status != tc.status || !slices.Equal(tool.Arguments, []string{"argument with spaces", "--literal"}) {
				t.Fatalf("resolver selection changed: %+v", tool)
			}
			if tc.status == "unavailable" && tool.Error == "" || tc.status == "available" && tool.Path != executable {
				t.Fatalf("resolver availability lost: %+v", tool)
			}
		})
	}
}
