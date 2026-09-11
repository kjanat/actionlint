package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	if err := writeDoctor(&text, request, false, "never"); err != nil {
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
	if err := writeDoctor(&data, request, true, "never"); err != nil {
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

func TestDoctorHyperlinks(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("NO_COLOR", "1")
	name := "config répo #100%.yaml"
	if err := os.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := "." + string(filepath.Separator) + string(filepath.Separator) + name
	args := []string{"doctor", "--config", config, "--shellcheck", `"` + filepath.ToSlash(executable) + `"`}
	osc := regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)
	for _, tc := range []struct {
		name, no, force, mode string
		linked                bool
	}{
		{"redirected auto", "", "", "auto", false},
		{"forced mode", "1", "", "always", true},
		{"forced environment", "", "1", "auto", true},
		{"zero forces", "", "0", "auto", true},
		{"no hyperlinks", "1", "1", "auto", false},
		{"zero disables", "0", "1", "auto", false},
		{"never", "", "1", "never", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_HYPERLINKS", tc.no)
			t.Setenv("FORCE_HYPERLINKS", tc.force)
			got := testRunCommand("", append(args, "--hyperlinks="+tc.mode)...)
			plain := testRunCommand("", append(args, "--hyperlinks=never")...)
			if got.Status != 0 || got.Stderr != "" || osc.ReplaceAllString(got.Stdout, "") != plain.Stdout {
				t.Fatalf("links changed doctor values or alignment: %+v / %+v", got, plain)
			}
			links := osc.FindAllString(got.Stdout, -1)
			want := 0
			if tc.linked {
				want = 6
			}
			if len(links) != want {
				t.Fatalf("got %d OSC sequences, want %d: %q", len(links), want, got.Stdout)
			}
			if tc.linked && (!strings.Contains(got.Stdout, "/config%20r%C3%A9po%20%23100%25.yaml\x1b\\"+name) || !strings.Contains(got.Stdout, "\x1b]8;;file:")) {
				t.Fatalf("missing encoded configuration link: %q", got.Stdout)
			}
			jsonArgs := append([]string{"--json"}, args...)
			data := testRunCommand("", append(jsonArgs, "--hyperlinks="+tc.mode)...)
			if want := testRunCommand("", append(jsonArgs, "--hyperlinks=never")...); data != want || !json.Valid([]byte(data.Stdout)) {
				t.Fatalf("hyperlink controls changed JSON: %+v / %+v", data, want)
			}
		})
	}
}
